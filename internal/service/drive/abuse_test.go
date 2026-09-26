package drive

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Every report carries the share's passphrase and the good-faith declaration,
// names what it is about, and sends null for whatever was not said - the body
// Proton's own SDK sends.

// reportBody is what the last request carried, which a test expects to be a
// report sent to path.
func reportBody(t *testing.T, doer *stubDoer, path string) map[string]any {
	t.Helper()
	req := doer.last()
	if req.Method != "POST" || req.Path != path {
		t.Fatalf("the report went to %s %s, want POST %s", req.Method, req.Path, path)
	}
	body, ok := req.Body.(map[string]any)
	if !ok {
		t.Fatalf("the report carried %T", req.Body)
	}
	if body["BonaFide"] != true {
		t.Errorf("BonaFide = %v, and Proton takes no report without it", body["BonaFide"])
	}
	return body
}

func expect(t *testing.T, body map[string]any, want map[string]any) {
	t.Helper()
	for field, value := range want {
		if body[field] != value {
			t.Errorf("%s = %v, want %v", field, body[field], value)
		}
	}
}

// keyPacketOf is the key packet a message's session key is sealed in, which is
// what a membership or an invitation carries for its member.
func keyPacketOf(t *testing.T, armored string) []byte {
	t.Helper()
	msg, err := pgp.NewPGPMessageFromArmored(armored)
	if err != nil {
		t.Fatalf("read the message: %v", err)
	}
	split, err := msg.SplitMessage()
	if err != nil {
		t.Fatalf("split the message: %v", err)
	}
	return split.GetBinaryKeyPacket()
}

// asMember is what Proton answers about a share somebody shared with you: your
// membership, and the key packet it opens the share's passphrase with.
func (tr *tree) asMember(t *testing.T) (passphrase, sessionKey string) {
	t.Helper()
	var sh map[string]any
	if err := json.Unmarshal([]byte(tr.share), &sh); err != nil {
		t.Fatalf("read the canned share: %v", err)
	}
	armored := sh["Passphrase"].(string)
	kp := keyPacketOf(t, armored)
	sh["Memberships"] = []any{map[string]any{
		"Permissions": permView, "KeyPacket": base64.StdEncoding.EncodeToString(kp),
	}}
	tr.share = object(t, sh)

	sk, err := tr.addrKR.DecryptSessionKey(kp)
	if err != nil {
		t.Fatalf("open the key packet: %v", err)
	}
	msg, err := pgp.NewPGPMessageFromArmored(armored)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := tr.addrKR.Decrypt(msg, nil, pgp.GetUnixTime())
	if err != nil {
		t.Fatalf("open the passphrase: %v", err)
	}
	return base64.StdEncoding.EncodeToString(dec.GetBinary()), base64.StdEncoding.EncodeToString(sk.Key)
}

func TestAReportOnSomethingSharedWithYouCarriesTheKeyToIt(t *testing.T) {
	tr := newTree(t, shareTypeStandard, protonFolder, "Project")
	passphrase, sessionKey := tr.asMember(t)
	s, doer := tr.service(nil)
	dc, err := s.unlockShare(context.Background(), testShareID, testRootID, testVolumeID)
	if err != nil {
		t.Fatalf("unlockShare: %v", err)
	}
	res, err := s.ResolvePath(context.Background(), dc, "/")
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}

	if err := s.ReportAbuse(context.Background(), res, Abuse{Category: "malware"}); err != nil {
		t.Fatalf("ReportAbuse: %v", err)
	}

	expect(t, reportBody(t, doer, "/drive/report/share"), map[string]any{
		"AbuseCategory": "malware", "ShareID": testShareID, "LinkID": testRootID,
		"SharePassphrase": passphrase, "MemberSessionKey": sessionKey,
		"ReporterEmail": nil, "ReporterMessage": nil, "RevisionID": nil,
		"ShareURL": nil, "ShareURLPassword": nil,
	})
}

// A file of your own has nobody else's share to report it under, so nothing is
// sent.
func TestYourOwnFilesAreNotReported(t *testing.T) {
	tr := newTree(t, shareTypeMain, protonFolder, "")
	s, doer := tr.service(nil)
	dc, err := s.unlockShare(context.Background(), testShareID, testRootID, testVolumeID)
	if err != nil {
		t.Fatalf("unlockShare: %v", err)
	}
	res, err := s.ResolvePath(context.Background(), dc, "/")
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}

	err = s.ReportAbuse(context.Background(), res, Abuse{Category: "spam"})

	if err == nil || !strings.Contains(err.Error(), "Only something shared with you") {
		t.Fatalf("ReportAbuse = %v", err)
	}
	if doer.sent("POST", "/drive/report/share") {
		t.Error("a report on your own files was sent")
	}
}

// A link is reported through the endpoint that answers without an account, with
// the link and the whole of its password, under the share its root names.
func TestAReportInALinkCarriesTheLinkAndItsPassword(t *testing.T) {
	tree := newPublicTree(t, testURLPassword+"hunter2", "Project", protonFolder)
	tree.items[testRootID] = map[string]any{
		"Link":    map[string]any{"LinkID": testRootID, "Type": protonFolder},
		"Folder":  map[string]any{},
		"Sharing": map[string]any{"ShareID": "link-share", "ShareURLID": "url-1"},
	}
	s, doer := publicService(t, tree, proton.PublicLinkCustomPassword|proton.PublicLinkGeneratedPassword, nil, nil)
	tree.hold(t, testRootID, uploadedFile("invoice.exe", testLinkSigner, uploadedJustNow()))
	dc, err := s.OpenLink(context.Background(), LinkURL(testToken, testURLPassword), "hunter2")
	if err != nil {
		t.Fatalf("OpenLink: %v", err)
	}
	res, err := s.ResolvePath(context.Background(), dc, "/invoice.exe")
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}

	if err := s.ReportAbuse(context.Background(), res, Abuse{
		Category: "copyright", Email: "jane@proton.me", Message: "My photos",
	}); err != nil {
		t.Fatalf("ReportAbuse: %v", err)
	}

	expect(t, reportBody(t, doer, "/drive/unauth/report/share"), map[string]any{
		"AbuseCategory": "copyright", "ShareID": "link-share", "LinkID": testFileID,
		"SharePassphrase":  base64.StdEncoding.EncodeToString([]byte("the-share-passphrase")),
		"ShareURL":         LinkURL(testToken, testURLPassword),
		"ShareURLPassword": testURLPassword + "hunter2",
		"ReporterEmail":    "jane@proton.me", "ReporterMessage": "My photos",
		"MemberSessionKey": nil, "RevisionID": nil,
	})
}

// A version of a file is reported as the file, with the version named beside it.
func TestAReportOnARevisionNamesTheRevision(t *testing.T) {
	tr := newTree(t, shareTypeStandard, 2, "report.pdf")
	tr.asMember(t)
	s, doer := tr.service(map[string]string{
		"GET /drive/shares/" + testShareID + "/files/" + testRootID + "/revisions": object(t, map[string]any{
			"Revisions": []any{map[string]any{"ID": "rev-old", "State": 2}, map[string]any{"ID": "rev-now", "State": 1}},
		}),
	})
	dc, err := s.unlockShare(context.Background(), testShareID, testRootID, testVolumeID)
	if err != nil {
		t.Fatalf("unlockShare: %v", err)
	}
	rev, err := s.FindRevision(context.Background(), dc, "/", "rev-old")
	if err != nil {
		t.Fatalf("FindRevision: %v", err)
	}

	if err := s.ReportRevisionAbuse(context.Background(), rev, Abuse{Category: "malware"}); err != nil {
		t.Fatalf("ReportRevisionAbuse: %v", err)
	}

	expect(t, reportBody(t, doer, "/drive/report/share"), map[string]any{
		"LinkID": testRootID, "RevisionID": "rev-old", "ShareID": testShareID,
	})
}

// What an invitation offers is reported before it is accepted, with the key
// packet the invitation itself carries.
func TestAReportOnAnInvitationCarriesTheKeyItOffers(t *testing.T) {
	u := signedInAs(t, testAddrMail)
	addrKR := u.AddrKRs[testAddrID].Read
	shareKey, sharePass, _, sharePriv, err := genNodeKeys(addrKR, addrKR)
	if err != nil {
		t.Fatalf("generate a share key: %v", err)
	}
	shareKR, err := pgp.NewKeyRing(sharePriv)
	if err != nil {
		t.Fatal(err)
	}
	encName, err := encryptName("Holidays", shareKR, shareKR)
	if err != nil {
		t.Fatal(err)
	}
	kp := keyPacketOf(t, sharePass)
	sk, err := addrKR.DecryptSessionKey(kp)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := pgp.NewPGPMessageFromArmored(sharePass)
	if err != nil {
		t.Fatal(err)
	}
	passphrase, err := addrKR.Decrypt(msg, nil, pgp.GetUnixTime())
	if err != nil {
		t.Fatal(err)
	}
	doer := &stubDoer{routes: map[string]string{
		"GET /drive/v2/shares/invitations/inv-1": object(t, map[string]any{
			"Invitation": map[string]any{
				"InvitationID": "inv-1", "InviterEmail": "bob@proton.me", "InviteeEmail": testAddrMail,
				"Permissions": permView, "KeyPacket": base64.StdEncoding.EncodeToString(kp),
			},
			"Share": map[string]any{"ShareID": "invited-share", "VolumeID": testVolumeID, "ShareKey": shareKey, "Passphrase": sharePass},
			"Link":  map[string]any{"LinkID": "invited-link", "Name": encName, "Type": protonAlbum},
		}),
	}}
	s := New(doer, testKeys(u))

	inv, err := s.GetInvitation(context.Background(), "inv-1")
	if err != nil {
		t.Fatalf("GetInvitation: %v", err)
	}
	if inv.Name != "Holidays" {
		t.Errorf("the invitation offers %q, want the decrypted name", inv.Name)
	}
	if err := s.ReportInvitationAbuse(context.Background(), "inv-1", Abuse{Category: "spam"}); err != nil {
		t.Fatalf("ReportInvitationAbuse: %v", err)
	}

	expect(t, reportBody(t, doer, "/drive/report/share"), map[string]any{
		"AbuseCategory": "spam", "ShareID": "invited-share", "LinkID": "invited-link",
		"SharePassphrase":  base64.StdEncoding.EncodeToString(passphrase.GetBinary()),
		"MemberSessionKey": base64.StdEncoding.EncodeToString(sk.Key),
	})
	if doer.sent("POST", "/drive/v2/shares/invitations/inv-1/accept") {
		t.Error("reporting an invitation accepted it")
	}
}
