//go:build !windows

package fido

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io/fs"
	"iter"
	"sync"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/telesma-app/ctap/transport"
	"github.com/telesma-app/ctap/transport/ctaphid"
)

// A security key made of software, for both ceremonies to run against.
//
// It speaks CTAPHID over an in-memory device and signs with a key generated for
// the test, so the real CTAP stack, the real framing and the real answers are
// all exercised with nothing plugged into the machine. It is the closest thing
// to hardware that runs in CI, and it covers everything between the challenge
// arriving and the answer leaving except the USB bus.

func mustParse(t *testing.T, raw json.RawMessage) authentication {
	t.Helper()
	a, err := parseAuthentication(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return a
}

func mustParseCreation(t *testing.T, raw json.RawMessage) creation {
	t.Helper()
	c, err := parseCreation(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return c
}

// numbers is how Proton writes a byte string: as an array of numbers.
func numbers(b []byte) []int {
	out := make([]int, len(b))
	for i, v := range b {
		out[i] = int(v)
	}
	return out
}

// protonChallenge is an AuthenticationOptions in the shape Proton sends one:
// binary written as arrays of numbers, and the relying party named separately
// from the host that sent it.
func protonChallenge(challenge, credentialID []byte, rpID string) json.RawMessage {
	if rpID == "" {
		rpID = "account.proton.me"
	}
	raw, err := json.Marshal(map[string]any{
		"publicKey": map[string]any{
			"challenge": numbers(challenge),
			"rpId":      rpID,
			"timeout":   60000,
			"allowCredentials": []any{
				map[string]any{"id": numbers(credentialID), "type": "public-key"},
			},
			"userVerification": "discouraged",
		},
	})
	if err != nil {
		panic(err)
	}
	return raw
}

// protonRegistration is a RegistrationOptions in the shape Proton sends one.
func protonRegistration(challenge []byte, exclude [][]byte, rpID, attestation string) json.RawMessage {
	if rpID == "" {
		rpID = "account.proton.me"
	}
	excluded := make([]any, 0, len(exclude))
	for _, id := range exclude {
		excluded = append(excluded, map[string]any{"id": numbers(id), "type": "public-key"})
	}
	raw, err := json.Marshal(map[string]any{
		"publicKey": map[string]any{
			"rp":        map[string]any{"id": rpID, "name": "Proton"},
			"user":      map[string]any{"id": numbers([]byte("account")), "name": "alice@proton.me", "displayName": "Alice"},
			"challenge": numbers(challenge),
			"timeout":   60000,
			"pubKeyCredParams": []any{
				map[string]any{"type": "public-key", "alg": -7},
			},
			"excludeCredentials":     excluded,
			"authenticatorSelection": map[string]any{"userVerification": "discouraged"},
			"attestation":            attestation,
		},
	})
	if err != nil {
		panic(err)
	}
	return raw
}

func noKeys(context.Context) iter.Seq2[transport.Device, error] {
	return func(func(transport.Device, error) bool) {}
}

func unopenableKey(context.Context) iter.Seq2[transport.Device, error] {
	return func(yield func(transport.Device, error) bool) {
		yield(nil, fs.ErrPermission)
	}
}

// softKey is the key itself: what it signs with, what it already holds, and
// whatever the test wants it to refuse with.
type softKey struct {
	private        *ecdsa.PrivateKey
	credID         []byte
	refuseWith     transport.StatusCode
	omitCredential bool
	asked          bool
}

// model is what a real key says it is, which the "none" attestation takes back
// out of an answer.
var model = []byte{
	0xf8, 0xa0, 0x11, 0xf3, 0x8c, 0x0a, 0x4d, 0x15,
	0x80, 0x06, 0x17, 0x11, 0x1f, 0x9e, 0xdc, 0x7d,
}

func newSoftKey(t *testing.T) *softKey {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return &softKey{private: private, credID: []byte("a credential registered with Proton")}
}

// sign and enrol are the platform seams this key stands in for.
func (k *softKey) sign(ctx context.Context, a authentication, clientData []byte, p Prompts) (Assertion, error) {
	return assertVia(ctx, k.enumerate, a, clientData, p)
}

func (k *softKey) enrol(ctx context.Context, c creation, clientData []byte, p Prompts) (Attestation, error) {
	return registerVia(ctx, k.enumerate, c, clientData, p)
}

// enumerate plugs the key in. Each call is a fresh connection, because the
// transport that opens one closes it, and a key survives being unplugged.
func (k *softKey) enumerate(ctx context.Context) iter.Seq2[transport.Device, error] {
	return func(yield func(transport.Device, error) bool) {
		yield(ctaphid.Open(ctx, &softLink{key: k,
			outgoing: make(chan []byte, 64), closed: make(chan struct{})}))
	}
}

// verifies checks an assertion the way a relying party does.
func (k *softKey) verifies(authData, clientData, signature []byte) bool {
	hash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte{}, authData...), hash[:]...))
	return ecdsa.VerifyASN1(&k.private.PublicKey, digest[:], signature)
}

// softLink is one connection to a softKey, framing CTAPHID reports both ways.
type softLink struct {
	key *softKey

	mu       sync.Mutex
	incoming []byte
	pending  *message
	outgoing chan []byte
	closed   chan struct{}
	once     sync.Once
}

type message struct {
	cid  uint32
	cmd  byte
	want int
	data []byte
}

const (
	reportSize        = 64
	reportWithID      = reportSize + 1
	ctapHIDPing       = 0x01
	ctapHIDInit       = 0x06
	ctapHIDCBOR       = 0x10
	ctapHIDCancel     = 0x11
	cborMakeCredetial = 0x01
	cborGetAssert     = 0x02
	cborGetInfo       = 0x04
	assignedChannel   = 0x01020304
)

func (l *softLink) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *softLink) Read(ctx context.Context, report []byte) (int, error) {
	select {
	case packet := <-l.outgoing:
		return copy(report, packet), nil
	case <-l.closed:
		return 0, errors.New("security key unplugged")
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (l *softLink) Write(_ context.Context, report []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.incoming = append(l.incoming, report...)
	for len(l.incoming) >= reportWithID {
		// The first byte of every write is the HID report ID, which is zero for a
		// device with a single report.
		packet := l.incoming[1:reportWithID]
		l.incoming = l.incoming[reportWithID:]
		l.packet(packet)
	}
	return len(report), nil
}

func (l *softLink) packet(p []byte) {
	cid := binary.BigEndian.Uint32(p[:4])
	if p[4]&0x80 != 0 {
		length := int(binary.BigEndian.Uint16(p[5:7]))
		data := append([]byte{}, p[7:]...)
		if len(data) > length {
			data = data[:length]
		}
		l.pending = &message{cid: cid, cmd: p[4] &^ 0x80, want: length, data: data}
	} else if l.pending != nil {
		data := p[5:]
		if room := l.pending.want - len(l.pending.data); len(data) > room {
			data = data[:room]
		}
		l.pending.data = append(l.pending.data, data...)
	}
	if l.pending != nil && len(l.pending.data) >= l.pending.want {
		done := *l.pending
		l.pending = nil
		l.answer(done)
	}
}

func (l *softLink) answer(m message) {
	switch m.cmd {
	case ctapHIDInit:
		reply := append([]byte{}, m.data...)
		reply = binary.BigEndian.AppendUint32(reply, assignedChannel)
		// Protocol version 2, device version 1.0.0, and the one capability that
		// matters: this key speaks CBOR.
		reply = append(reply, 0x02, 0x01, 0x00, 0x00, 0x04)
		l.send(m.cid, ctapHIDInit, reply)
	case ctapHIDPing:
		l.send(m.cid, ctapHIDPing, m.data)
	case ctapHIDCBOR:
		l.send(m.cid, ctapHIDCBOR, l.cbor(m.data))
	case ctapHIDCancel:
		// A cancelled command is answered by the command itself, not by this.
	}
}

func (l *softLink) cbor(payload []byte) []byte {
	if len(payload) == 0 {
		return []byte{byte(transport.CTAP1_ERR_INVALID_LENGTH)}
	}
	switch payload[0] {
	case cborGetInfo:
		info, err := cbor.Marshal(map[int]any{
			1: []string{"FIDO_2_0", "FIDO_2_1"},
			3: model,
			4: map[string]bool{"up": true, "plat": false},
			6: []int{2, 1},
		})
		if err != nil {
			return []byte{byte(transport.CTAP2_ERR_INVALID_CBOR)}
		}
		return append([]byte{byte(transport.CTAP2_OK)}, info...)
	case cborGetAssert:
		return l.assertion(payload[1:])
	case cborMakeCredetial:
		return l.credential(payload[1:])
	default:
		return []byte{byte(transport.CTAP1_ERR_INVALID_COMMAND)}
	}
}

func (l *softLink) assertion(params []byte) []byte {
	l.key.asked = true
	if l.key.refuseWith != transport.CTAP2_OK {
		return []byte{byte(l.key.refuseWith)}
	}
	var request struct {
		RPID           string `cbor:"1,keyasint"`
		ClientDataHash []byte `cbor:"2,keyasint"`
		AllowList      []struct {
			Type string `cbor:"type"`
			ID   []byte `cbor:"id"`
		} `cbor:"3,keyasint,omitempty"`
	}
	if err := cbor.Unmarshal(params, &request); err != nil {
		return []byte{byte(transport.CTAP2_ERR_INVALID_CBOR)}
	}
	held := false
	for _, allowed := range request.AllowList {
		if string(allowed.ID) == string(l.key.credID) {
			held = true
		}
	}
	if !held {
		return []byte{byte(transport.CTAP2_ERR_NO_CREDENTIALS)}
	}

	// Authenticator data: the hash of the relying party, the flag saying somebody
	// was here, and a counter. What a real key signs, in the same order.
	rpIDHash := sha256.Sum256([]byte(request.RPID))
	authData := append(rpIDHash[:], flagUserPresent)
	authData = binary.BigEndian.AppendUint32(authData, 1)

	signature, err := l.key.signOver(authData, request.ClientDataHash)
	if err != nil {
		return []byte{byte(transport.CTAP1_ERR_OTHER)}
	}

	response := map[int]any{2: authData, 3: signature}
	if !l.key.omitCredential {
		response[1] = map[string]any{"type": "public-key", "id": l.key.credID}
	}
	return answerWith(response)
}

// The two authenticator-data flags a soft key ever sets: somebody was there,
// and a new credential is described in what follows.
const (
	flagUserPresent           = 0x01
	flagAttestedCredentialSet = 0x40
)

// credential is authenticatorMakeCredential: the key makes a credential and
// attests to it, as a key with no attestation certificate does - by signing
// with the credential itself.
func (l *softLink) credential(params []byte) []byte {
	l.key.asked = true
	if l.key.refuseWith != transport.CTAP2_OK {
		return []byte{byte(l.key.refuseWith)}
	}
	var request struct {
		ClientDataHash []byte `cbor:"1,keyasint"`
		RP             struct {
			ID string `cbor:"id"`
		} `cbor:"2,keyasint"`
		User struct {
			ID []byte `cbor:"id"`
		} `cbor:"3,keyasint"`
		Algorithms []struct {
			Type      string `cbor:"type"`
			Algorithm int    `cbor:"alg"`
		} `cbor:"4,keyasint"`
		ExcludeList []struct {
			Type string `cbor:"type"`
			ID   []byte `cbor:"id"`
		} `cbor:"5,keyasint,omitempty"`
	}
	if err := cbor.Unmarshal(params, &request); err != nil {
		return []byte{byte(transport.CTAP2_ERR_INVALID_CBOR)}
	}
	for _, excluded := range request.ExcludeList {
		if string(excluded.ID) == string(l.key.credID) {
			return []byte{byte(transport.CTAP2_ERR_CREDENTIAL_EXCLUDED)}
		}
	}
	if len(request.Algorithms) == 0 || request.Algorithms[0].Algorithm != -7 {
		return []byte{byte(transport.CTAP2_ERR_UNSUPPORTED_ALGORITHM)}
	}

	rpIDHash := sha256.Sum256([]byte(request.RP.ID))
	authData := append(rpIDHash[:], flagUserPresent|flagAttestedCredentialSet)
	authData = binary.BigEndian.AppendUint32(authData, 1)
	attested, err := l.key.attestedCredential()
	if err != nil {
		return []byte{byte(transport.CTAP1_ERR_OTHER)}
	}
	authData = append(authData, attested...)

	signature, err := l.key.signOver(authData, request.ClientDataHash)
	if err != nil {
		return []byte{byte(transport.CTAP1_ERR_OTHER)}
	}
	return answerWith(map[int]any{
		1: "packed",
		2: authData,
		3: map[string]any{"alg": -7, "sig": signature},
	})
}

// attestedCredential is the credential as it travels inside authenticator
// data: which make of key it is, what the credential is called, and its public
// key as COSE writes one.
func (k *softKey) attestedCredential() ([]byte, error) {
	// The uncompressed point, which is a marker byte and then the two
	// coordinates COSE writes separately.
	point, err := k.private.PublicKey.Bytes()
	if err != nil {
		return nil, err
	}
	public, err := cbor.Marshal(map[int]any{
		1:  2,
		3:  -7,
		-1: 1,
		-2: point[1:33],
		-3: point[33:65],
	})
	if err != nil {
		return nil, err
	}
	out := append([]byte{}, model...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(k.credID)))
	out = append(out, k.credID...)
	return append(out, public...), nil
}

// signOver signs what every ceremony signs: the authenticator data with the
// hash of the client data after it.
func (k *softKey) signOver(authData, clientDataHash []byte) ([]byte, error) {
	digest := sha256.Sum256(append(append([]byte{}, authData...), clientDataHash...))
	return ecdsa.SignASN1(rand.Reader, k.private, digest[:])
}

func answerWith(response map[int]any) []byte {
	encoded, err := cbor.Marshal(response)
	if err != nil {
		return []byte{byte(transport.CTAP2_ERR_INVALID_CBOR)}
	}
	return append([]byte{byte(transport.CTAP2_OK)}, encoded...)
}

// send frames a reply the way CTAPHID does: one initialisation packet carrying
// the length, then continuation packets numbered from zero.
func (l *softLink) send(cid uint32, cmd byte, payload []byte) {
	packet := make([]byte, reportSize)
	binary.BigEndian.PutUint32(packet, cid)
	packet[4] = cmd | 0x80
	binary.BigEndian.PutUint16(packet[5:], uint16(len(payload)))
	sent := copy(packet[7:], payload)
	l.outgoing <- packet

	for sequence := byte(0); sent < len(payload); sequence++ {
		packet := make([]byte, reportSize)
		binary.BigEndian.PutUint32(packet, cid)
		packet[4] = sequence
		sent += copy(packet[5:], payload[sent:])
		l.outgoing <- packet
	}
}
