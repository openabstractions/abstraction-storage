package service

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestNamingDoesNotClaimVerifiedContent(t *testing.T) {
	_, c, d, _ := fixture(t)
	opened, _ := c.Open(d)
	read, _ := c.Read(opened.Resource.Handle, 0, 65536)
	if opened.Resource.Verification != "unverified" || read.Chunk == nil || !read.Chunk.Eof {
		t.Fatal(opened, read)
	}
	actual := fmt.Sprintf("sha256:%x", sha256.Sum256(read.Chunk.Data))
	if actual == d {
		t.Fatal("fixture must demonstrate mislabeled content")
	}
}
