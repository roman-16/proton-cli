package passfile

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

var (
	ErrBackupLocked     = errors.New("passfile: the archive is locked")
	ErrBackupPassphrase = errors.New("passfile: the archive does not open with this passphrase")
	ErrBackupUnreadable = errors.New("passfile: the archive does not read back")
)

type Backup struct {
	Doc   *ExportDocument
	z     *zip.ReadCloser
	files map[string]*zip.File
}

func OpenBackup(path, passphrase string) (*Backup, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	b, err := readBackup(z, path, passphrase)
	if err != nil {
		_ = z.Close()
		return nil, err
	}
	return b, nil
}

func readBackup(z *zip.ReadCloser, path, passphrase string) (*Backup, error) {
	document, files := archiveParts(z)
	b := &Backup{z: z, files: files}
	if document == nil {
		return nil, ErrBackupUnreadable
	}
	body, err := readZipEntry(source{path: path}, document)
	if err != nil {
		return nil, errors.Join(ErrBackupUnreadable, err)
	}
	if strings.HasSuffix(document.Name, ".pgp") {
		if passphrase == "" {
			return nil, ErrBackupLocked
		}
		if body, err = decryptExport(string(body), passphrase); err != nil {
			return nil, ErrBackupPassphrase
		}
	}
	b.Doc = &ExportDocument{}
	if err := json.Unmarshal(body, b.Doc); err != nil {
		return nil, errors.Join(ErrBackupUnreadable, err)
	}
	return b, nil
}

func (b *Backup) File(entry string) (ArchiveFile, bool) {
	f, ok := b.files[entry]
	if !ok {
		return ArchiveFile{}, false
	}
	return ArchiveFile{Entry: entry, Write: func(w io.Writer) error {
		r, err := f.Open()
		if err != nil {
			return err
		}
		defer func() { _ = r.Close() }()
		_, err = io.Copy(w, r)
		return err
	}}, true
}

func (b *Backup) Close() error { return b.z.Close() }
