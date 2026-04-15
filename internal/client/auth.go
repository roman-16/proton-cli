package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ProtonMail/go-srp"
)

type authInfoResponse struct {
	Code            int
	Modulus         string
	ServerEphemeral string
	Version         int
	Salt            string
	SRPSession      string
}

type authResponse struct {
	Code         int
	UID          string
	AccessToken  string
	RefreshToken string
	ServerProof  string
	TwoFA        struct {
		Enabled int
	} `json:"2FA"`
}

type sessionResponse struct {
	Code         int
	UID          string
	AccessToken  string
	RefreshToken string
}

// createSession creates an unauthenticated session via POST /auth/v4/sessions.
// This is the first step in the web client auth flow and avoids human verification.
func (c *Client) createSession(ctx context.Context) (*sessionResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/auth/v4/sessions", strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-pm-appversion", c.appVersion)
	req.Header.Set("x-enforce-unauthsession", "true")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("session creation failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result sessionResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("session response parse error: %w", err)
	}

	if result.Code != 1000 {
		return nil, fmt.Errorf("session creation returned code %d: %s", result.Code, string(body))
	}

	return &result, nil
}

// getAuthInfo fetches SRP parameters within the existing session.
func (c *Client) getAuthInfo(ctx context.Context, username string) (*authInfoResponse, error) {
	reqBody, _ := json.Marshal(map[string]string{"Username": username})

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/core/v4/auth/info", strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-pm-appversion", c.appVersion)
	req.Header.Set("x-pm-uid", c.uid)
	req.Header.Set("Authorization", "Bearer "+c.acc)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth info failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result authInfoResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("auth info parse error: %w", err)
	}

	if result.Code != 1000 {
		return nil, fmt.Errorf("auth info returned code %d: %s", result.Code, string(body))
	}

	return &result, nil
}

// srpLogin performs SRP authentication within the existing session.
func (c *Client) srpLogin(ctx context.Context, username string, password []byte, info *authInfoResponse) (*authResponse, error) {
	srpAuth, err := srp.NewAuth(info.Version, username, password, info.Salt, info.Modulus, info.ServerEphemeral)
	if err != nil {
		return nil, fmt.Errorf("SRP setup failed: %w", err)
	}

	proofs, err := srpAuth.GenerateProofs(2048)
	if err != nil {
		return nil, fmt.Errorf("SRP proof generation failed: %w", err)
	}

	reqBody, _ := json.Marshal(map[string]interface{}{
		"Username":        username,
		"ClientProof":     base64.StdEncoding.EncodeToString(proofs.ClientProof),
		"ClientEphemeral": base64.StdEncoding.EncodeToString(proofs.ClientEphemeral),
		"SRPSession":      info.SRPSession,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/core/v4/auth", strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-pm-appversion", c.appVersion)
	req.Header.Set("x-pm-uid", c.uid)
	req.Header.Set("Authorization", "Bearer "+c.acc)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SRP auth failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result authResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("auth response parse error: %w", err)
	}

	if result.Code != 1000 {
		return nil, fmt.Errorf("auth returned code %d: %s", result.Code, string(body))
	}

	// Verify server proof.
	serverProof, err := base64.StdEncoding.DecodeString(result.ServerProof)
	if err != nil {
		return nil, fmt.Errorf("server proof decode error: %w", err)
	}
	if !bytes.Equal(serverProof, proofs.ExpectedServerProof) {
		return nil, fmt.Errorf("server proof verification failed")
	}

	return &result, nil
}

// auth2FA submits the TOTP code for two-factor authentication.
func (c *Client) auth2FA(ctx context.Context, totp string) error {
	reqBody, _ := json.Marshal(map[string]string{"TwoFactorCode": totp})

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/core/v4/auth/2fa", strings.NewReader(string(reqBody)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-pm-appversion", c.appVersion)
	req.Header.Set("x-pm-uid", c.uid)
	req.Header.Set("Authorization", "Bearer "+c.acc)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("2FA request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var result struct {
		Code int
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("2FA response parse error: %w", err)
	}

	if result.Code != 1000 {
		return fmt.Errorf("2FA failed (code %d): %s", result.Code, string(body))
	}

	return nil
}

// UnlockPasswordScope performs SRP re-authentication against the current session
// to unlock the "locked" scope needed for sensitive operations (e.g. calendar delete).
// This is equivalent to the web client's "Enter your password" modal.
func (c *Client) UnlockPasswordScope(ctx context.Context, username string, password []byte) error {
	// Step 1: Get auth info within the existing session.
	info, err := c.getAuthInfo(ctx, username)
	if err != nil {
		return fmt.Errorf("scope unlock failed: %w", err)
	}

	// Step 2: SRP proof.
	srpAuth, err := srp.NewAuth(info.Version, username, password, info.Salt, info.Modulus, info.ServerEphemeral)
	if err != nil {
		return fmt.Errorf("scope unlock SRP setup failed: %w", err)
	}

	proofs, err := srpAuth.GenerateProofs(2048)
	if err != nil {
		return fmt.Errorf("scope unlock SRP proof failed: %w", err)
	}

	reqBody, _ := json.Marshal(map[string]interface{}{
		"ClientProof":     base64.StdEncoding.EncodeToString(proofs.ClientProof),
		"ClientEphemeral": base64.StdEncoding.EncodeToString(proofs.ClientEphemeral),
		"SRPSession":      info.SRPSession,
	})

	c.mu.RLock()
	uid := c.uid
	acc := c.acc
	c.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, "PUT", c.baseURL+"/core/v4/users/password", strings.NewReader(string(reqBody)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-pm-appversion", c.appVersion)
	req.Header.Set("x-pm-uid", uid)
	req.Header.Set("Authorization", "Bearer "+acc)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("scope unlock request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var result struct {
		Code int
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("scope unlock parse error: %w", err)
	}

	if result.Code != 1000 {
		return fmt.Errorf("scope unlock failed (code %d): %s", result.Code, string(body))
	}

	return nil
}

// Login performs the full web-style auth flow:
// 1. Create unauthenticated session
// 2. Get SRP auth info
// 3. SRP login
// 4. 2FA (if required)
func (c *Client) Login(ctx context.Context, username string, password []byte, totp string) error {
	// Step 1: Create unauthenticated session.
	sess, err := c.createSession(ctx)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	c.mu.Lock()
	c.uid = sess.UID
	c.acc = sess.AccessToken
	c.ref = sess.RefreshToken
	c.mu.Unlock()

	// Step 2: Get auth info.
	info, err := c.getAuthInfo(ctx, username)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	// Step 3: SRP login.
	auth, err := c.srpLogin(ctx, username, password, info)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	c.mu.Lock()
	c.uid = auth.UID
	c.acc = auth.AccessToken
	c.ref = auth.RefreshToken
	c.mu.Unlock()

	// Step 4: 2FA if required.
	const hasTOTP = 1
	if auth.TwoFA.Enabled&hasTOTP != 0 {
		if totp == "" {
			return fmt.Errorf("account requires 2FA but no TOTP code provided (set PROTON_TOTP or --totp)")
		}
		if err := c.auth2FA(ctx, totp); err != nil {
			return fmt.Errorf("login failed: %w", err)
		}
	}

	return nil
}
