// Package digest is the one Go owner of the content digest form (storage
// STO-D1): "sha256:" followed by exactly 64 lowercase hexadecimal digits.
//
// Valid is the strict check every service boundary applies. Normalize is the
// one lenient reader, for digests written by foreign stores (Ollama names its
// blobs "sha256-<hex>"). Hex and File produce the strict form.
package digest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
)

// Prefix is the algorithm label of the one digest form.
const Prefix = "sha256:"

// Valid reports whether d is exactly "sha256:" and 64 lowercase hex digits.
// Bare hex, uppercase hex, surrounding space and other text are refused, so a
// caller-supplied digest cannot name a path before it reaches a lookup.
func Valid(d string) bool {
	if len(d) != len(Prefix)+64 || d[:len(Prefix)] != Prefix {
		return false
	}
	return lowerHex(d[len(Prefix):])
}

// Normalize reads a digest spelled by another implementation and returns its
// 64 lowercase hex digits, or "" when it is not a recognisable sha256 digest.
// It accepts surrounding space, any case, and the "sha256:", "sha256-" or no
// label. An unrecognised digest returns "", so two unreadable digests never
// compare equal.
func Normalize(d string) string {
	s := strings.ToLower(strings.TrimSpace(d))
	s = strings.TrimPrefix(s, Prefix)
	s = strings.TrimPrefix(s, "sha256-")
	if len(s) != 64 || !lowerHex(s) {
		return ""
	}
	return s
}

// Hex returns the strict digest of a sha256 sum.
func Hex(sum []byte) string {
	return Prefix + hex.EncodeToString(sum)
}

// File hashes the file at path and returns its size and strict digest.
// progress, when not nil, is called after each read with the bytes done and
// the file's size. A cancelled ctx stops the read.
func File(ctx context.Context, path string, progress func(done, total int64)) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	total := int64(0)
	if st, serr := f.Stat(); serr == nil {
		total = st.Size()
	}
	h := sha256.New()
	buf := make([]byte, 1<<20)
	var done int64
	for {
		if err := ctx.Err(); err != nil {
			return done, "", err
		}
		n, rerr := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			done += int64(n)
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			return done, Hex(h.Sum(nil)), nil
		}
		if rerr != nil {
			return done, "", rerr
		}
	}
}

func lowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
