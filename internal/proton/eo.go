package proton

import "net/url"

// A message sent to an address outside Proton is not in any mailbox: Proton
// holds it under an id of its own, sealed with a password its sender chose and
// passed on some other way, and serves it to whoever brings both.
//
// So none of these requests identifies an account, whether or not one is signed
// in. The first is public - it hands out the token the password unwraps - and
// the rest carry that token, which is the whole of what Proton will answer them
// for. Mirrors packages/shared/lib/api/eo.ts in Proton's own clients.

// EOTokenRequest asks for the token that opens a message, still encrypted to
// the password.
func EOTokenRequest(id string) Request {
	return Request{
		Method: "GET", Path: "/mail/v4/eo/token/" + url.PathEscape(id),
		anonymous: true,
	}
}

// EOMessageRequest asks for the message itself: its envelope, its body and the
// public key an answer to it is sealed to.
func EOMessageRequest(id, token string) Request {
	return Request{
		Method: "GET", Path: "/mail/v4/eo/message",
		anonymous: true, headers: eoHeaders(id, token),
	}
}

// EOAttachmentRequest asks for one attachment's bytes, as encrypted as they
// were sent.
func EOAttachmentRequest(id, token, attachmentID string) Request {
	return Request{
		Method: "GET", Path: "/mail/v4/eo/attachment/" + url.PathEscape(attachmentID),
		anonymous: true, headers: eoHeaders(id, token),
	}
}

// EOReplyRequest sends an answer back to whoever sent the message.
func EOReplyRequest(id, token string, form []byte, contentType string) Request {
	return Request{
		Method: "POST", Path: "/mail/v4/eo/reply", Body: form, ContentType: contentType,
		anonymous: true, headers: eoHeaders(id, token),
	}
}

// eoHeaders is how such a request says what it is about. The token stands where
// a session's bearer token would and is not one - Proton reads it as the
// message's own credential - and the id names the message it opens.
func eoHeaders(id, token string) map[string]string {
	return map[string]string{"Authorization": token, "x-eo-uid": id}
}
