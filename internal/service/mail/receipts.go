package mail

import (
	"context"

	"github.com/roman-16/proton-cli/internal/proton"
)

// A read receipt has two halves and they live in different places. The sender's
// request is a flag on the message they compose, and it reaches the other end as
// a header on the message that arrives - so a copy this account sent carries the
// asking, and the copy that arrived carries the answering.

// Receipt is where one message stands with read receipts.
type Receipt struct {
	// Subject names the message, for a refusal that has to say which one.
	Subject string
	// Requested says a read receipt was asked for.
	Requested bool
	// Sent says one has already gone back.
	Sent bool
	// To is the address the sender asked to be told at.
	To string
	// Outgoing marks a copy this account sent.
	Outgoing bool
}

// Answerable reports whether there is still a receipt for this reader to send.
func (r Receipt) Answerable() bool { return r.Requested && !r.Sent && !r.Outgoing }

// receiptOf reads a message's standing off the message itself.
func receiptOf(m rawMessage) Receipt {
	to := m.parsedHeader("Disposition-Notification-To")
	return Receipt{
		Subject:   m.Subject,
		Requested: to != "" || m.Flags&flagReceiptRequest != 0,
		Sent:      m.Flags&flagReceiptSent != 0,
		To:        ParseRecipient(to).Address,
		Outgoing:  m.isSent(),
	}
}

// ReceiptOf is where one message stands with read receipts.
func (s *Service) ReceiptOf(ctx context.Context, id string) (Receipt, error) {
	m, err := s.fetchMessageRaw(ctx, id)
	if err != nil {
		return Receipt{}, err
	}
	return receiptOf(*m), nil
}

// SendReceipt tells a message's sender that it was read.
func (s *Service) SendReceipt(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/mail/v4/messages/" + id + "/receipt",
	}, nil)
}
