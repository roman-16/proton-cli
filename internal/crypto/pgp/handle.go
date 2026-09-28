package pgp

import (
	"github.com/ProtonMail/gopenpgp/v3/constants"
	gopenpgp "github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/ProtonMail/gopenpgp/v3/profile"
)

// PGP is the handle every key, encryption, decryption and signature in the CLI
// is made through. Its profile is the one Proton's own clients use, so what is
// written here is what they write.
var PGP = gopenpgp.PGPWithProfile(profile.ProtonV1())

// Text and binary are told apart by name below because the difference is what
// another client checks: a text signature is over line endings made canonical,
// and a text message comes back with them restored. Getting it wrong fails
// nowhere here, only in the client that opens what was written.

// GenerateKey makes a key the way Proton's clients make one for an address,
// a calendar or a Drive node.
func GenerateKey(name, email string) (*gopenpgp.Key, error) {
	return PGP.KeyGeneration().AddUserId(name, email).New().GenerateKey()
}

// EncryptText encrypts text to a key ring, signing it inside the message when
// signer is not nil.
func EncryptText(to, signer *gopenpgp.KeyRing, text string) (*gopenpgp.PGPMessage, error) {
	enc, err := PGP.Encryption().Recipients(to).SigningKeys(signer).Utf8().New()
	if err != nil {
		return nil, err
	}
	return enc.Encrypt([]byte(text))
}

// EncryptBinary encrypts bytes to a key ring, signing them inside the message
// when signer is not nil.
func EncryptBinary(to, signer *gopenpgp.KeyRing, data []byte) (*gopenpgp.PGPMessage, error) {
	enc, err := PGP.Encryption().Recipients(to).SigningKeys(signer).New()
	if err != nil {
		return nil, err
	}
	return enc.Encrypt(data)
}

// EncryptTextWithSessionKey is the data packet of text encrypted under a
// session key, signed inside when signer is not nil.
func EncryptTextWithSessionKey(sk *gopenpgp.SessionKey, signer *gopenpgp.KeyRing, text string) ([]byte, error) {
	enc, err := PGP.Encryption().SessionKey(sk).SigningKeys(signer).Utf8().New()
	if err != nil {
		return nil, err
	}
	msg, err := enc.Encrypt([]byte(text))
	if err != nil {
		return nil, err
	}
	return msg.BinaryDataPacket(), nil
}

// EncryptBinaryWithSessionKey is the data packet of bytes encrypted under a
// session key, signed inside when signer is not nil.
func EncryptBinaryWithSessionKey(sk *gopenpgp.SessionKey, signer *gopenpgp.KeyRing, data []byte) ([]byte, error) {
	enc, err := PGP.Encryption().SessionKey(sk).SigningKeys(signer).New()
	if err != nil {
		return nil, err
	}
	msg, err := enc.Encrypt(data)
	if err != nil {
		return nil, err
	}
	return msg.BinaryDataPacket(), nil
}

// EncryptSessionKey is the key packet that opens a session key for a key ring.
func EncryptSessionKey(to *gopenpgp.KeyRing, sk *gopenpgp.SessionKey) ([]byte, error) {
	enc, err := PGP.Encryption().Recipients(to).New()
	if err != nil {
		return nil, err
	}
	return enc.EncryptSessionKey(sk)
}

// DecryptSessionKey opens a key packet with a key ring.
func DecryptSessionKey(kr *gopenpgp.KeyRing, keyPacket []byte) (*gopenpgp.SessionKey, error) {
	dec, err := PGP.Decryption().DecryptionKeys(kr).New()
	if err != nil {
		return nil, err
	}
	return dec.DecryptSessionKey(keyPacket)
}

// EncryptSessionKeyWithPassword is the key packet that opens a session key
// for whoever holds the password.
func EncryptSessionKeyWithPassword(password []byte, sk *gopenpgp.SessionKey) ([]byte, error) {
	enc, err := PGP.Encryption().Password(password).New()
	if err != nil {
		return nil, err
	}
	return enc.EncryptSessionKey(sk)
}

// DecryptSessionKeyWithPassword opens a key packet with a password.
func DecryptSessionKeyWithPassword(password, keyPacket []byte) (*gopenpgp.SessionKey, error) {
	dec, err := PGP.Decryption().Password(password).New()
	if err != nil {
		return nil, err
	}
	return dec.DecryptSessionKey(keyPacket)
}

// Decrypt opens a message holding bytes. With a verifier, SignatureError says
// whether the signature inside it checked out.
func Decrypt(kr, verifier *gopenpgp.KeyRing, message []byte, encoding int8) (*gopenpgp.VerifiedDataResult, error) {
	dec, err := PGP.Decryption().DecryptionKeys(kr).VerificationKeys(verifier).New()
	if err != nil {
		return nil, err
	}
	return dec.Decrypt(message, encoding)
}

// DecryptText opens a message holding text. With a verifier, SignatureError
// says whether the signature inside it checked out.
func DecryptText(kr, verifier *gopenpgp.KeyRing, message []byte, encoding int8) (*gopenpgp.VerifiedDataResult, error) {
	dec, err := PGP.Decryption().DecryptionKeys(kr).VerificationKeys(verifier).Utf8().New()
	if err != nil {
		return nil, err
	}
	return dec.Decrypt(message, encoding)
}

// SignatureError is the verdict on the signature inside a decrypted message,
// checked against verifier alone. Decryption lends its own keys to the check,
// so a message signed by one of them reads as verified even to a verifier that
// does not hold that key; here it reads as what it is, unverified.
func SignatureError(res *gopenpgp.VerifiedDataResult, verifier *gopenpgp.KeyRing) error {
	if err := res.SignatureError(); err != nil {
		return err
	}
	if signer := res.SignedByKey(); signer != nil && verifier != nil {
		for _, key := range verifier.GetKeys() {
			if key.GetFingerprint() == signer.GetFingerprint() {
				return nil
			}
		}
	}
	return gopenpgp.SignatureVerificationError{Status: constants.SIGNATURE_NO_VERIFIER, Message: "No matching signature"}
}

// DecryptWithSessionKey opens a data packet holding bytes.
func DecryptWithSessionKey(sk *gopenpgp.SessionKey, dataPacket []byte) ([]byte, error) {
	dec, err := PGP.Decryption().SessionKey(sk).New()
	if err != nil {
		return nil, err
	}
	res, err := dec.Decrypt(dataPacket, gopenpgp.Bytes)
	if err != nil {
		return nil, err
	}
	return res.Bytes(), nil
}

// DecryptTextWithSessionKey opens a data packet holding text.
func DecryptTextWithSessionKey(sk *gopenpgp.SessionKey, dataPacket []byte) (string, error) {
	dec, err := PGP.Decryption().SessionKey(sk).Utf8().New()
	if err != nil {
		return "", err
	}
	res, err := dec.Decrypt(dataPacket, gopenpgp.Bytes)
	if err != nil {
		return "", err
	}
	return res.String(), nil
}

// EncryptTextWithPassword is text encrypted to a password, armored, which is
// how a message is left for somebody who holds no key.
func EncryptTextWithPassword(password []byte, text string) (string, error) {
	enc, err := PGP.Encryption().Password(password).Utf8().New()
	if err != nil {
		return "", err
	}
	msg, err := enc.Encrypt([]byte(text))
	if err != nil {
		return "", err
	}
	return msg.Armor()
}

// DecryptTextWithPassword opens an armored message holding text with the
// password it was encrypted to.
func DecryptTextWithPassword(password []byte, armored string) (string, error) {
	dec, err := PGP.Decryption().Password(password).Utf8().New()
	if err != nil {
		return "", err
	}
	res, err := dec.Decrypt([]byte(armored), gopenpgp.Armor)
	if err != nil {
		return "", err
	}
	return res.String(), nil
}

// DecryptWithPassword opens a message holding bytes with the password it was
// encrypted to.
func DecryptWithPassword(password, message []byte, encoding int8) ([]byte, error) {
	dec, err := PGP.Decryption().Password(password).New()
	if err != nil {
		return nil, err
	}
	res, err := dec.Decrypt(message, encoding)
	if err != nil {
		return nil, err
	}
	return res.Bytes(), nil
}

// SignText is a detached text signature, armored or binary as encoding says.
func SignText(kr *gopenpgp.KeyRing, text string, encoding int8) ([]byte, error) {
	return SignTextInContext(kr, text, nil, encoding)
}

// SignTextInContext is SignText made for one context, which a verifier that
// requires it checks.
func SignTextInContext(kr *gopenpgp.KeyRing, text string, context *gopenpgp.SigningContext, encoding int8) ([]byte, error) {
	signer, err := PGP.Sign().SigningKeys(kr).SigningContext(context).Detached().Utf8().New()
	if err != nil {
		return nil, err
	}
	return signer.Sign([]byte(text), encoding)
}

// SignTextArmored is SignText armored, as the API takes a signature.
func SignTextArmored(kr *gopenpgp.KeyRing, text string) (string, error) {
	sig, err := SignText(kr, text, gopenpgp.Armor)
	return string(sig), err
}

// SignBinary is a detached binary signature, armored or binary as encoding
// says.
func SignBinary(kr *gopenpgp.KeyRing, data []byte, encoding int8) ([]byte, error) {
	return SignBinaryInContext(kr, data, nil, encoding)
}

// SignBinaryInContext is SignBinary made for one context, which a verifier
// that requires it checks.
func SignBinaryInContext(kr *gopenpgp.KeyRing, data []byte, context *gopenpgp.SigningContext, encoding int8) ([]byte, error) {
	signer, err := PGP.Sign().SigningKeys(kr).SigningContext(context).Detached().New()
	if err != nil {
		return nil, err
	}
	return signer.Sign(data, encoding)
}

// SignBinaryArmored is SignBinary armored, as the API takes a signature.
func SignBinaryArmored(kr *gopenpgp.KeyRing, data []byte) (string, error) {
	sig, err := SignBinary(kr, data, gopenpgp.Armor)
	return string(sig), err
}

// VerifyText checks a detached signature over text. The error is the
// signature's verdict, or why the signature could not be read.
func VerifyText(kr *gopenpgp.KeyRing, text string, signature []byte, encoding int8) error {
	return VerifyTextInContext(kr, text, signature, encoding, nil)
}

// VerifyTextInContext is VerifyText requiring the context the signature was
// made for.
func VerifyTextInContext(kr *gopenpgp.KeyRing, text string, signature []byte, encoding int8, context *gopenpgp.VerificationContext) error {
	verifier, err := PGP.Verify().VerificationKeys(kr).VerificationContext(context).Utf8().New()
	if err != nil {
		return err
	}
	res, err := verifier.VerifyDetached([]byte(text), signature, encoding)
	if err != nil {
		return err
	}
	return res.SignatureError()
}

// VerifyBinary checks a detached signature over bytes. The error is the
// signature's verdict, or why the signature could not be read.
func VerifyBinary(kr *gopenpgp.KeyRing, data, signature []byte, encoding int8) error {
	return VerifyBinaryInContext(kr, data, signature, encoding, nil)
}

// VerifyBinaryInContext is VerifyBinary requiring the context the signature
// was made for.
func VerifyBinaryInContext(kr *gopenpgp.KeyRing, data, signature []byte, encoding int8, context *gopenpgp.VerificationContext) error {
	verifier, err := PGP.Verify().VerificationKeys(kr).VerificationContext(context).New()
	if err != nil {
		return err
	}
	res, err := verifier.VerifyDetached(data, signature, encoding)
	if err != nil {
		return err
	}
	return res.SignatureError()
}
