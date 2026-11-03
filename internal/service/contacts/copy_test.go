package contacts

import (
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/vcard"
)

func TestSplitCardsReadsBackTheCardsTheCopyWrote(t *testing.T) {
	cards := []string{
		vcard.Document([]string{"BEGIN:VCARD\r\nVERSION:4.0\r\nUID:jane\r\nFN:Jane Roe\r\nNOTE:" + strings.Repeat("long note ", 20) + "\r\nEND:VCARD"}, nil),
		vcard.Document([]string{"BEGIN:VCARD\r\nVERSION:4.0\r\nFN:No UID\r\nEND:VCARD"}, nil),
	}
	var file strings.Builder
	for _, card := range cards {
		file.WriteString(card + "\r\n")
	}
	got := splitCards(file.String())
	if len(got) != len(cards) {
		t.Fatalf("read back %d cards, want %d", len(got), len(cards))
	}
	for i := range cards {
		if got[i] != cards[i] {
			t.Errorf("card %d read back as\n%s\nwant\n%s", i, got[i], cards[i])
		}
	}
	if cardKey(got[0]) != "jane" || cardKey(got[1]) != got[1] {
		t.Errorf("keys = %q, %q", cardKey(got[0]), cardKey(got[1]))
	}
	if splitCards("") != nil {
		t.Error("an empty file holds cards")
	}
}
