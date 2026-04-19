package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/ProtonMail/go-srp"
)

type authInfo struct {
	Code            int
	Modulus         string
	ServerEphemeral string
	Version         int
	Salt            string
	SRPSession      string
}

type authResp struct {
	Code         int
	UID          string
	AccessToken  string
	RefreshToken string
	ServerProof  string
	TwoFA        struct {
		Enabled int
	} `json:"2FA"`
}

// Login performs the full web-client auth flow (unauth session → SRP → 2FA).
func (c *Client) Login(ctx context.Context, username string, password []byte, totp string) error {
	sess, err := c.createSession(ctx)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	c.mu.Lock()
	c.uid, c.acc, c.ref = sess.UID, sess.AccessToken, sess.RefreshToken
	c.mu.Unlock()

	info, err := c.getAuthInfo(ctx, username)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	auth, err := c.srpLogin(ctx, username, password, info)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	c.mu.Lock()
	c.uid, c.acc, c.ref = auth.UID, auth.AccessToken, auth.RefreshToken
	c.mu.Unlock()

	if auth.TwoFA.Enabled&1 != 0 {
		if totp == "" {
			return fmt.Errorf("account requires 2FA but no TOTP code provided")
		}
		if err := c.auth2FA(ctx, totp); err != nil {
			return fmt.Errorf("login: %w", err)
		}
	}
	return nil
}

// UnlockPasswordScope performs SRP re-auth within the current session,
// unlocking the "locked" scope required for sensitive mutations.
func (c *Client) UnlockPasswordScope(ctx context.Context, username string, password []byte) error {
	info, err := c.getAuthInfo(ctx, username)
	if err != nil {
		return fmt.Errorf("scope unlock: %w", err)
	}
	srpAuth, err := srp.NewAuth(info.Version, username, password, info.Salt, info.Modulus, info.ServerEphemeral)
	if err != nil {
		return fmt.Errorf("scope unlock: %w", err)
	}
	proofs, err := srpAuth.GenerateProofs(2048)
	if err != nil {
		return fmt.Errorf("scope unlock: %w", err)
	}
	body, _ := json.Marshal(map[string]any{
		"ClientProof":     base64.StdEncoding.EncodeToString(proofs.ClientProof),
		"ClientEphemeral": base64.StdEncoding.EncodeToString(proofs.ClientEphemeral),
		"SRPSession":      info.SRPSession,
	})
	if _, err := c.rawAuth(ctx, "PUT", "/core/v4/users/password", body); err != nil {
		return err
	}
	return nil
}

func (c *Client) createSession(ctx context.Context) (*authResp, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/auth/v4/sessions", bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-pm-appversion", c.app)
	req.Header.Set("x-enforce-unauthsession", "true")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	var r authResp
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("session parse: %w", err)
	}
	if r.Code != 1000 {
		return nil, fmt.Errorf("session creation code %d: %s", r.Code, string(b))
	}
	return &r, nil
}

func (c *Client) getAuthInfo(ctx context.Context, username string) (*authInfo, error) {
	body, _ := json.Marshal(map[string]string{"Username": username})
	raw, err := c.rawAuth(ctx, "POST", "/core/v4/auth/info", body)
	if err != nil {
		return nil, err
	}
	var r authInfo
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("auth info parse: %w", err)
	}
	if r.Code != 1000 {
		return nil, fmt.Errorf("auth info code %d: %s", r.Code, string(raw))
	}
	return &r, nil
}

func (c *Client) srpLogin(ctx context.Context, username string, password []byte, info *authInfo) (*authResp, error) {
	srpAuth, err := srp.NewAuth(info.Version, username, password, info.Salt, info.Modulus, info.ServerEphemeral)
	if err != nil {
		return nil, fmt.Errorf("SRP setup: %w", err)
	}
	proofs, err := srpAuth.GenerateProofs(2048)
	if err != nil {
		return nil, fmt.Errorf("SRP proofs: %w", err)
	}
	body, _ := json.Marshal(map[string]any{
		"Username":        username,
		"ClientProof":     base64.StdEncoding.EncodeToString(proofs.ClientProof),
		"ClientEphemeral": base64.StdEncoding.EncodeToString(proofs.ClientEphemeral),
		"SRPSession":      info.SRPSession,
	})
	raw, err := c.rawAuth(ctx, "POST", "/core/v4/auth", body)
	if err != nil {
		return nil, err
	}
	var r authResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("auth parse: %w", err)
	}
	if r.Code != 1000 {
		return nil, fmt.Errorf("auth code %d: %s", r.Code, string(raw))
	}
	serverProof, err := base64.StdEncoding.DecodeString(r.ServerProof)
	if err != nil {
		return nil, fmt.Errorf("server proof decode: %w", err)
	}
	if !bytes.Equal(serverProof, proofs.ExpectedServerProof) {
		return nil, fmt.Errorf("server proof verification failed")
	}
	return &r, nil
}

func (c *Client) auth2FA(ctx context.Context, totp string) error {
	body, _ := json.Marshal(map[string]string{"TwoFactorCode": totp})
	raw, err := c.rawAuth(ctx, "POST", "/core/v4/auth/2fa", body)
	if err != nil {
		return err
	}
	var r struct{ Code int }
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("2FA parse: %w", err)
	}
	if r.Code != 1000 {
		return fmt.Errorf("2FA code %d: %s", r.Code, string(raw))
	}
	return nil
}

// rawAuth sends a request with current session headers and returns the body.
// Used for auth-flow endpoints that bypass the normal Do retry path.
func (c *Client) rawAuth(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	c.mu.RLock()
	uid, acc := c.uid, c.acc
	c.mu.RUnlock()
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-pm-appversion", c.app)
	if uid != "" {
		req.Header.Set("x-pm-uid", uid)
	}
	if acc != "" {
		req.Header.Set("Authorization", "Bearer "+acc)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}
