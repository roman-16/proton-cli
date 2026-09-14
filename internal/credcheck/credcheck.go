// Package credcheck asks whether a password appears in the public corpus of
// leaked credentials Proton publishes, without telling anybody the password.
//
// The corpus is split into buckets by the first six hexadecimal characters of a
// password's SHA-1, and a bucket holds the remaining thirty-four characters of
// every hash in it. So what is sent is the bucket - one of sixteen million - and
// what comes back is a list this machine matches against locally. Neither the
// password nor its whole hash ever leaves.
//
// SHA-1 is the corpus's index, not a security decision here. The password is not
// being stored, compared or authenticated against - it is being looked up, and a
// lookup has to use the key the index was built with, so no other algorithm can
// answer the question at all. Nothing rests on the hash being hard to invert or
// free of collisions, only on a bucket being too crowded to point at anybody.
//
// A static analyser reads "password into SHA-1" and reports weak password
// hashing. Both halves of that are true and the conclusion is not; there is
// nothing to harden, because the only alternative to this hash is not asking.
//
// It is the only thing in this CLI that talks to a host which is not the Proton
// API, so it takes its own client and holds no session: the corpus is public and
// answers an unauthenticated request.
package credcheck

import (
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Host is where the corpus is published. It is named in the help of the command
// that reaches it, so anyone can see in advance what a check would talk to.
const Host = "credential-check.protonweb.com"

const base = "https://" + Host

// prefixLength is how many hexadecimal characters of the hash name the bucket.
const prefixLength = 6

// maxBucket caps what one bucket may be. A bucket is a few tens of kilobytes;
// anything past this is not the answer to the question that was asked.
const maxBucket = 8 << 20

// Compromised reports whether the password is in the corpus.
//
// A bucket nobody has published is a bucket with nothing in it, which is the
// ordinary answer for a password that has not leaked.
func Compromised(ctx context.Context, client *http.Client, password string) (bool, error) {
	sum := sha1.Sum([]byte(password))
	hash := strings.ToUpper(hex.EncodeToString(sum[:]))
	prefix, suffix := hash[:prefixLength], hash[prefixLength:]

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bucketURL(prefix), nil)
	if err != nil {
		return false, err
	}
	// The corpus pads its answers when asked, so a bucket's size says nothing
	// about how many hashes are in it.
	req.Header.Set("Add-Padding", "true")

	res, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("reaching %s: %w", Host, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if res.StatusCode != http.StatusOK {
		return false, fmt.Errorf("%s answered %s", Host, res.Status)
	}

	found, err := holds(res.Body, suffix)
	if err != nil {
		return false, fmt.Errorf("reading the answer from %s: %w", Host, err)
	}
	return found, nil
}

// bucketURL is where one bucket is published: the prefix again as three pairs of
// directories, and then the file.
func bucketURL(prefix string) string {
	return fmt.Sprintf("%s/split/sha1/%s/%s/%s/%s.gz",
		base, prefix[0:2], prefix[2:4], prefix[4:6], prefix)
}

// holds reports whether a bucket names this hash. Each line is a hash suffix and
// how often it was seen, separated by a colon; only the suffix is wanted.
func holds(body io.Reader, suffix string) (bool, error) {
	gz, err := gzip.NewReader(io.LimitReader(body, maxBucket))
	if err != nil {
		return false, err
	}
	defer func() { _ = gz.Close() }()
	text, err := io.ReadAll(io.LimitReader(gz, maxBucket))
	if err != nil {
		return false, err
	}
	for line := range strings.Lines(string(text)) {
		candidate, _, _ := strings.Cut(strings.TrimSpace(line), ":")
		if strings.EqualFold(candidate, suffix) {
			return true, nil
		}
	}
	return false, nil
}
