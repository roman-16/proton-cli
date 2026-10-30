package export

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/roman-16/proton-cli/internal/localfile"
)

const stateDir = ".proton-cli"

var (
	ErrNotACopy     = errors.New("export: the directory holds other files")
	ErrNotDirectory = errors.New("export: the path is not a directory")
)

type OtherAccount struct{ Email string }

func (o *OtherAccount) Error() string { return "export: a copy of " + o.Email }

type Copy struct {
	root string
	lock *localfile.Lock
}

type owner struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
}

func Check(root string) error {
	fi, err := os.Stat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return ErrNotDirectory
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	if len(entries) == 0 || localfile.Exists(ownerPath(root)) {
		return nil
	}
	if len(entries) == 1 && entries[0].Name() == stateDir {
		return nil
	}
	return ErrNotACopy
}

func Open(root, userID, email string) (*Copy, error) {
	if err := Check(root); err != nil {
		return nil, err
	}
	if err := localfile.EnsureDir(filepath.Join(root, stateDir)); err != nil {
		return nil, err
	}
	lock, err := localfile.Claim(filepath.Join(root, stateDir, "lock"))
	if err != nil {
		return nil, err
	}
	c := &Copy{root: root, lock: lock}
	if err := c.claimFor(userID, email); err != nil {
		c.Close()
		return nil, err
	}
	if err := localfile.Sweep(root); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Copy) claimFor(userID, email string) error {
	raw, err := os.ReadFile(ownerPath(c.root))
	if errors.Is(err, fs.ErrNotExist) {
		doc, err := json.Marshal(owner{UserID: userID, Email: email})
		if err != nil {
			return err
		}
		return localfile.Publish(ownerPath(c.root), doc, time.Time{})
	}
	if err != nil {
		return err
	}
	var was owner
	if err := json.Unmarshal(raw, &was); err != nil {
		return err
	}
	if was.UserID != userID {
		return &OtherAccount{Email: was.Email}
	}
	return nil
}

func ownerPath(root string) string { return filepath.Join(root, stateDir, "account.json") }

func (c *Copy) Root() string { return c.root }

func (c *Copy) Path(rel string) string { return filepath.Join(c.root, filepath.FromSlash(rel)) }

func (c *Copy) Ledger(app App) (*Ledger, error) {
	return openLedger(filepath.Join(c.root, stateDir, string(app)+".jsonl"), c)
}

func (c *Copy) Close() {
	c.lock.Release()
}
