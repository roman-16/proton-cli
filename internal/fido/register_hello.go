//go:build windows && webauthn

package fido

import (
	"context"
	"errors"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Registering a key on Windows, called straight into webauthn.dll.
//
// The DLL is part of Windows and is loaded when it is used, so nothing about
// this asks the person to install anything: the same library that already
// answers a sign-in answers this. What is written out here is the shape of the
// call - six structures and a pointer to be filled in - because a WebAuthn
// structure is versioned and the caller says which version it wrote. Every
// version this uses is the lowest one, present since the API's first release,
// so what is sent is what any Windows since 10 1903 can read and nothing here
// depends on which build is running.

var (
	webauthnDLL             = windows.NewLazyDLL("webauthn.dll")
	procMakeCredential      = webauthnDLL.NewProc("WebAuthNAuthenticatorMakeCredential")
	procFreeAttestation     = webauthnDLL.NewProc("WebAuthNFreeCredentialAttestation")
	credentialTypePublicKey = windows.StringToUTF16Ptr("public-key")
	hashAlgorithmSHA256     = windows.StringToUTF16Ptr("SHA-256")
)

// The version of each structure this writes, all of them the first. A newer
// Windows accepts an older structure and reads only the fields it declares.
const (
	versionRPEntity             = 1
	versionUserEntity           = 1
	versionCredentialParameter  = 1
	versionClientData           = 1
	versionCredentialEx         = 1
	versionMakeCredential       = 3
	versionAttestationTransport = 3
)

// register hands the registration to Windows, which draws the dialog and talks
// to the key.
func register(_ context.Context, c creation, clientData []byte, p Prompts) (Attestation, error) {
	hwnd, err := dialog()
	if err != nil {
		return Attestation{}, err
	}

	rp := helloRPEntity{
		Version: versionRPEntity,
		ID:      windows.StringToUTF16Ptr(c.rpID),
		Name:    windows.StringToUTF16Ptr(c.rpName),
	}
	user := helloUserEntity{
		Version:     versionUserEntity,
		IDLen:       uint32(len(c.user.id)),
		ID:          unsafe.SliceData(c.user.id),
		Name:        windows.StringToUTF16Ptr(c.user.name),
		DisplayName: windows.StringToUTF16Ptr(c.user.displayName),
	}

	algorithms := make([]helloCredentialParameter, 0, len(c.algorithms))
	for _, alg := range c.algorithms {
		algorithms = append(algorithms, helloCredentialParameter{
			Version:   versionCredentialParameter,
			Type:      credentialTypePublicKey,
			Algorithm: int32(alg),
		})
	}
	parameters := helloCredentialParameters{
		Count:      uint32(len(algorithms)),
		Parameters: unsafe.SliceData(algorithms),
	}

	data := helloClientData{
		Version:   versionClientData,
		JSONLen:   uint32(len(clientData)),
		JSON:      unsafe.SliceData(clientData),
		HashAlgID: hashAlgorithmSHA256,
	}

	// The keys already on the account, so that registering one of them a second
	// time is refused by the key itself rather than making a credential nobody
	// can tell from the one beside it.
	excluded := make([]helloCredentialEx, 0, len(c.exclude))
	for _, id := range c.exclude {
		excluded = append(excluded, helloCredentialEx{
			Version: versionCredentialEx,
			IDLen:   uint32(len(id)),
			ID:      unsafe.SliceData(id),
			Type:    credentialTypePublicKey,
		})
	}
	pointers := make([]*helloCredentialEx, len(excluded))
	for i := range excluded {
		pointers[i] = &excluded[i]
	}
	exclude := helloCredentialList{
		Count:       uint32(len(pointers)),
		Credentials: unsafe.SliceData(pointers),
	}

	options := helloMakeCredentialOptions{
		Version:             versionMakeCredential,
		TimeoutMilliseconds: uint32(c.timeout().Milliseconds()),
		// A key somebody plugs in, and never the one built into the machine.
		// Proton registers both under the same setting, but a built-in
		// authenticator belongs to this computer alone, and registering the
		// machine somebody happens to be at as their second factor is not what
		// the command says it does.
		AuthenticatorAttachment: attachmentCrossPlatform,
		UserVerification:        verificationRequirement(c.terms),
		AttestationConveyance:   conveyance(c),
	}
	if exclude.Count > 0 {
		options.ExcludeCredentialList = &exclude
	}

	var attestation *helloCredentialAttestation
	p.touch("Follow the prompt from Windows to register your security key.")
	r1, _, _ := procMakeCredential.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&rp)),
		uintptr(unsafe.Pointer(&user)),
		uintptr(unsafe.Pointer(&parameters)),
		uintptr(unsafe.Pointer(&data)),
		uintptr(unsafe.Pointer(&options)),
		uintptr(unsafe.Pointer(&attestation)),
	)
	runtime.KeepAlive(rp)
	runtime.KeepAlive(user)
	runtime.KeepAlive(algorithms)
	runtime.KeepAlive(data)
	runtime.KeepAlive(clientData)
	runtime.KeepAlive(excluded)
	runtime.KeepAlive(pointers)
	runtime.KeepAlive(options)
	if hr := windows.Handle(r1); hr != windows.S_OK {
		return Attestation{}, answered(windows.Errno(hr))
	}
	if attestation == nil {
		return Attestation{}, errors.New("Windows reported a registration with nothing in it")
	}
	defer func() { _, _, _ = procFreeAttestation.Call(uintptr(unsafe.Pointer(attestation))) }()

	return Attestation{
		AttestationObject: taken(attestation.AttestationObject, attestation.AttestationObjectLen),
		CredentialID:      taken(attestation.CredentialID, attestation.CredentialIDLen),
		Transports:        transports(attestation),
	}, nil
}

// conveyance is how much Windows is to ask the key to say about itself. What
// the relying party asked for is passed on, so the attestation Windows returns
// is already the one Proton wants and nothing here has to take a statement back
// out of it.
func conveyance(c creation) uint32 {
	if c.wantsAttestation() {
		return uint32(conveyanceDirect)
	}
	return uint32(conveyanceNone)
}

// transports is the one way Windows was able to reach the key. It arrived with
// the third version of the structure, which is the version this asks for.
func transports(a *helloCredentialAttestation) []string {
	if a.Version < versionAttestationTransport {
		return nil
	}
	named := []struct {
		flag uint32
		name string
	}{
		{1 << 0, transportUSB},
		{1 << 1, "nfc"},
		{1 << 2, "ble"},
		{1 << 4, "internal"},
		{1 << 5, "hybrid"},
		{1 << 6, "smart-card"},
	}
	var out []string
	for _, t := range named {
		if a.UsedTransport&t.flag != 0 {
			out = append(out, t.name)
		}
	}
	return out
}

// taken copies a buffer Windows allocated into one Go owns, before the answer
// is freed.
func taken(p *uint8, length uint32) []byte {
	if p == nil || length == 0 {
		return nil
	}
	return append([]byte{}, unsafe.Slice(p, length)...)
}

// The structures webauthn.dll reads and writes, in the layout a 64-bit Windows
// gives them. Only the fields up to the declared version are present: the DLL
// writes a structure at least as long as the one it was told about, and reading
// no further is what makes an older declaration safe against a newer Windows.

type helloRPEntity struct {
	Version uint32
	ID      *uint16
	Name    *uint16
	Icon    *uint16
}

type helloUserEntity struct {
	Version     uint32
	IDLen       uint32
	ID          *uint8
	Name        *uint16
	Icon        *uint16
	DisplayName *uint16
}

type helloCredentialParameter struct {
	Version   uint32
	Type      *uint16
	Algorithm int32
	_         [4]byte
}

type helloCredentialParameters struct {
	Count      uint32
	Parameters *helloCredentialParameter
}

type helloClientData struct {
	Version   uint32
	JSONLen   uint32
	JSON      *uint8
	HashAlgID *uint16
}

type helloCredentialEx struct {
	Version    uint32
	IDLen      uint32
	ID         *uint8
	Type       *uint16
	Transports uint32
	_          [4]byte
}

type helloCredentialList struct {
	Count       uint32
	Credentials **helloCredentialEx
}

// helloCredentials is the exclusion list as the first version of the API took
// it, before pExcludeCredentialList replaced it. It is always empty and is here
// because the field sits in the middle of the options.
type helloCredentials struct {
	Count       uint32
	Credentials *byte
}

type helloExtensions struct {
	Count      uint32
	Extensions *byte
}

type helloMakeCredentialOptions struct {
	Version                 uint32
	TimeoutMilliseconds     uint32
	CredentialList          helloCredentials
	Extensions              helloExtensions
	AuthenticatorAttachment uint32
	RequireResidentKey      int32
	UserVerification        uint32
	AttestationConveyance   uint32
	Flags                   uint32
	CancellationID          *[16]byte
	ExcludeCredentialList   *helloCredentialList
}

type helloCredentialAttestation struct {
	Version               uint32
	FormatType            *uint16
	AuthenticatorDataLen  uint32
	AuthenticatorData     *uint8
	AttestationLen        uint32
	Attestation           *uint8
	AttestationDecodeType uint32
	AttestationDecode     *byte
	AttestationObjectLen  uint32
	AttestationObject     *uint8
	CredentialIDLen       uint32
	CredentialID          *uint8
	Extensions            helloExtensions
	UsedTransport         uint32
}
