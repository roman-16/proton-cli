package passfile

import "testing"

func TestAChromeExportIsRead(t *testing.T) {
	body := "name,url,username,password,note\n" +
		"proton.me,https://account.proton.me/switch,nobody@proton.me,proton123,\n" +
		"no url,,someone,proton123,a note\n"
	doc := opened(t, written(t, "chrome.csv", []byte(body)), "chrome", nil)

	if doc.Count() != 2 {
		t.Fatalf("read %d items, want 2", doc.Count())
	}
	if doc.Vaults[0].Name != "" {
		t.Errorf("a file with no folders named a vault: %q", doc.Vaults[0].Name)
	}
	first := doc.Vaults[0].Items[0].Item.GetContent().GetLogin()
	if first.GetItemEmail() != "nobody@proton.me" || first.GetItemUsername() != "" {
		t.Errorf("an address came back as %v", first)
	}
	second := doc.Vaults[0].Items[1].Item.GetContent().GetLogin()
	if second.GetItemUsername() != "someone" || second.GetItemEmail() != "" {
		t.Errorf("a user name came back as %v", second)
	}
	if got := doc.Vaults[0].Items[1].Item.GetMetadata().GetNote(); got != "a note" {
		t.Errorf("the note came back as %q", got)
	}
}

// Edge and Brave write Chrome's file, so one reader answers for all three.
func TestEveryChromiumBrowserReadsTheSameFile(t *testing.T) {
	path := written(t, "passwords.csv", []byte("name,url,username,password\nx,https://x.test,jane,pw\n"))
	for _, browser := range []string{"brave", "chrome", "edge"} {
		if doc := opened(t, path, browser, nil); doc.Count() != 1 {
			t.Errorf("%s read %d items, want 1", browser, doc.Count())
		}
	}
}

// Firefox writes the dates in milliseconds, and keeps the account you sign in to
// Firefox with among the passwords.
func TestAFirefoxExportIsRead(t *testing.T) {
	body := `"url","username","password","httpRealm","formActionOrigin","guid","timeCreated","timeLastUsed","timePasswordChanged"` + "\n" +
		`"https://account.proton.me","nobody@example.com","proton123",,"","{a}","1679064121003","1679064121003","1679064140519"` + "\n" +
		`"chrome://FirefoxAccounts","fxa","secret",,"","{b}","1679064121003","1679064121003","1679064121003"` + "\n"
	doc := opened(t, written(t, "logins.csv", []byte(body)), "firefox", nil)

	if doc.Count() != 1 {
		t.Fatalf("read %d items, want 1", doc.Count())
	}
	entry := doc.Vaults[0].Items[0]
	if entry.CreateTime != 1679064121 || entry.ModifyTime != 1679064140 {
		t.Errorf("the dates came back as %d and %d", entry.CreateTime, entry.ModifyTime)
	}
	if entry.Name != "account.proton.me" {
		t.Errorf("the item is called %q, want the host it is for", entry.Name)
	}
}

// Safari before Sonoma wrote four columns and now writes six, so the two it
// gained are wanted rather than required.
func TestASafariExportIsRead(t *testing.T) {
	body := "Title,URL,Username,Password,Notes,OTPAuth\n" +
		"2fa.example.com,https://2fa.example.com/,scanned,pass,a note,otpauth://totp/x?secret=JBSWY3DPEHPK3PXP\n"
	doc := opened(t, written(t, "safari.csv", []byte(body)), "safari", nil)

	login := doc.Vaults[0].Items[0].Item.GetContent().GetLogin()
	if login.GetTotpUri() == "" {
		t.Error("the one-time code did not come back")
	}
	old := written(t, "old.csv", []byte("Title,URL,Username,Password\nx,https://x.test,jane,pw\n"))
	if doc := opened(t, old, "safari", nil); doc.Count() != 1 {
		t.Errorf("an older Safari export read %d items", doc.Count())
	}
}

// Apple Passwords writes Safari's file.
func TestAnApplePasswordsExportIsRead(t *testing.T) {
	path := written(t, "passwords.csv", []byte("Title,URL,Username,Password,Notes,OTPAuth\nx,https://x.test,jane,pw,,\n"))
	if doc := opened(t, path, "apple-passwords", nil); doc.Count() != 1 {
		t.Errorf("read %d items, want 1", doc.Count())
	}
}

// A file from another program is refused by name rather than read into
// nonsense.
func TestABrowserRefusesSomebodyElsesFile(t *testing.T) {
	path := written(t, "bitwarden.json", []byte(`{"items":[]}`))
	if _, err := Open(path, "chrome", nil); err == nil {
		t.Error("a JSON file was read as a Chrome export")
	}
}
