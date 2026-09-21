//go:build windows && webauthn

package fido

import (
	"errors"
	"fmt"

	"github.com/go-ctap/winhello"
	"github.com/go-ctap/winhello/window"
	"golang.org/x/sys/windows"
)

// What both ceremonies do on Windows, which owns every authenticator on the
// machine: a USB key plugged into it, and the fingerprint or PIN that Windows
// Hello itself is. Reaching the USB key directly is not an option here -
// Windows reserves that to processes running as administrator, which nothing
// this CLI does should need to be.

// dialog is the window Windows will draw its prompt in front of.
//
// A terminal has the foreground when somebody is typing in it, which is the
// only moment either ceremony runs. The absent API is reported as this platform
// being unable to reach a key at all, because that is what it amounts to.
func dialog() (windows.HWND, error) {
	if winhello.InitError != nil {
		return 0, ErrUnsupported
	}
	hwnd, err := window.GetForegroundWindow()
	if err != nil {
		return 0, fmt.Errorf("asking Windows for a security key: %w", err)
	}
	return hwnd, nil
}

// How Windows numbers what WebAuthn names in words. Both ceremonies take the
// same values, and only the ones this CLI can mean are here.
const (
	attachmentCrossPlatform      = 2
	verificationRequired         = 1
	verificationDiscouraged      = 3
	conveyanceNone          uint = 1
	conveyanceDirect        uint = 3
)

func verificationRequirement(t terms) uint32 {
	if t.needsVerification() {
		return verificationRequired
	}
	return verificationDiscouraged
}

// errTimeout is what the WebAuthn API returns when its dialog waited long
// enough, as the Win32 timeout wrapped into an HRESULT.
const errTimeout = windows.Errno(0x80070000 | uint32(windows.ERROR_TIMEOUT))

// answered turns what Windows said into what this package promises. Anything
// else is passed through: an unrecognised HRESULT is better read as itself than
// flattened into a sentence that may be wrong.
func answered(err error) error {
	switch {
	case errors.Is(err, windows.Errno(windows.NTE_USER_CANCELLED)), errors.Is(err, errTimeout):
		return ErrDenied
	case errors.Is(err, windows.Errno(windows.NTE_DEVICE_NOT_FOUND)):
		return ErrNoDevice
	case errors.Is(err, windows.Errno(windows.NTE_NOT_FOUND)):
		return ErrNoCredential
	case errors.Is(err, windows.Errno(windows.NTE_EXISTS)):
		return ErrRegistered
	case errors.Is(err, windows.Errno(windows.NTE_NOT_SUPPORTED)):
		return ErrUnsupported
	}
	return err
}
