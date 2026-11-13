package selfmanage

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"

	gopenpgp "github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/minio/selfupdate"
	"github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/progress"
	"golang.org/x/mod/semver"
)

const (
	owner = "roman-16"
	repo  = "proton-cli"
)

const releasesURL = "https://github.com/" + owner + "/" + repo + "/releases"

const changelogURL = "https://raw.githubusercontent.com/" + owner + "/" + repo + "/main/CHANGELOG.md"

//go:embed release-key.asc
var releaseKey string

var (
	ErrUnsigned = errors.New("the release carries no signature")
	ErrForged   = errors.New("the release's signature does not match the release key")
)

func Changelog(ctx context.Context, client *http.Client) ([]byte, error) {
	return get(ctx, client, changelogURL, nil)
}

func AssetName(goos, goarch string) (string, error) {
	if goarch == "amd64" || goarch == "arm64" {
		switch goos {
		case "linux", "darwin":
			return fmt.Sprintf("%s_%s_%s", repo, goos, goarch), nil
		case "windows":
			return fmt.Sprintf("%s_windows_%s.exe", repo, goarch), nil
		}
	}
	return "", fmt.Errorf("no prebuilt binary for %s/%s", goos, goarch)
}

// LatestVersion follows the /releases/latest redirect rather than asking the
// GitHub API, whose unauthenticated rate limit a shared address runs into.
func LatestVersion(ctx context.Context, client *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, releasesURL+"/latest", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	final := resp.Request.URL.Path
	idx := strings.LastIndex(final, "/tag/")
	if idx < 0 {
		return "", fmt.Errorf("could not determine latest version from %q", resp.Request.URL)
	}
	v := strings.TrimPrefix(final[idx+len("/tag/"):], "v")
	if v == "" {
		return "", fmt.Errorf("could not determine latest version from %q", resp.Request.URL)
	}
	return v, nil
}

func IsNewer(latest, current string) bool {
	lv := "v" + strings.TrimPrefix(latest, "v")
	cv := "v" + strings.TrimPrefix(current, "v")
	if !semver.IsValid(lv) {
		return false
	}
	if !semver.IsValid(cv) {
		return true
	}
	return semver.Compare(lv, cv) > 0
}

func ExpectedChecksum(checksums []byte, filename string) (string, error) {
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == filename {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum for %s in checksums.txt", filename)
}

func Download(ctx context.Context, client *http.Client, version string, prog progress.Sink) ([]byte, error) {
	asset, err := AssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	version = strings.TrimPrefix(version, "v")
	base := releasesURL + "/download/v" + version

	sums, err := get(ctx, client, base+"/checksums.txt", nil)
	if err != nil {
		return nil, fmt.Errorf("download checksums.txt: %w", err)
	}
	sig, err := get(ctx, client, base+"/checksums.txt.sig", nil)
	var status *statusError
	if errors.As(err, &status) && status.code == http.StatusNotFound {
		return nil, ErrUnsigned
	}
	if err != nil {
		return nil, fmt.Errorf("download checksums.txt.sig: %w", err)
	}
	if err := verifyRelease(releaseKey, sums, sig, version); err != nil {
		return nil, err
	}
	want, err := ExpectedChecksum(sums, asset)
	if err != nil {
		return nil, err
	}

	bin, err := get(ctx, client, base+"/"+asset, progress.Of(prog))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", asset, err)
	}
	got := sha256.Sum256(bin)
	if hex.EncodeToString(got[:]) != want {
		return nil, fmt.Errorf("checksum mismatch for %s (expected %s, got %s)", asset, want, hex.EncodeToString(got[:]))
	}
	return bin, nil
}

func verifyRelease(key string, sums, sig []byte, version string) error {
	k, err := gopenpgp.NewKeyFromArmored(key)
	if err != nil {
		return fmt.Errorf("read the release key: %w", err)
	}
	kr, err := gopenpgp.NewKeyRing(k)
	if err != nil {
		return fmt.Errorf("read the release key: %w", err)
	}
	if err := pgp.VerifyBinary(kr, sums, sig, gopenpgp.Bytes); err != nil {
		return fmt.Errorf("%w: %v", ErrForged, err)
	}
	if !listsVersion(sums, version) {
		return fmt.Errorf("%w: checksums.txt lists no archive of %s", ErrForged, version)
	}
	return nil
}

func listsVersion(checksums []byte, version string) bool {
	prefix := repo + "_" + version + "_"
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.HasPrefix(fields[1], prefix) {
			return true
		}
	}
	return false
}

func Apply(bin []byte, exePath string) error {
	return selfupdate.Apply(bytes.NewReader(bin), selfupdate.Options{TargetPath: exePath})
}

type statusError struct {
	url    string
	status string
	code   int
}

func (e *statusError) Error() string { return "GET " + e.url + ": " + e.status }

func get(ctx context.Context, client *http.Client, url string, prog progress.Sink) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, &statusError{url: url, status: resp.Status, code: resp.StatusCode}
	}
	if prog == nil {
		return io.ReadAll(resp.Body)
	}
	prog.Start(resp.ContentLength, "Downloading")
	defer prog.Done()
	return io.ReadAll(io.TeeReader(resp.Body, counter{prog}))
}

type counter struct{ sink progress.Sink }

func (c counter) Write(p []byte) (int, error) {
	c.sink.Add(int64(len(p)))
	return len(p), nil
}
