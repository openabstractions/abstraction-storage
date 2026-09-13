package client

import (
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
	"testing"
)

func TestChunkCombinations(t *testing.T) {
	r := Resource{Size: 3}
	good := func() api.ReadResult {
		return api.ReadResult{Outcome: "data", Chunk: &api.Chunk{Offset: 0, Total: 3, Data: []byte("abc"), Eof: true}}
	}
	if e := validateRead(good(), r, 0, 3); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*api.ReadResult){func(x *api.ReadResult) { x.Outcome = "gap" }, func(x *api.ReadResult) { x.Chunk = nil }, func(x *api.ReadResult) { x.Chunk.Offset = 1 }, func(x *api.ReadResult) { x.Chunk.Total = 4 }, func(x *api.ReadResult) { x.Chunk.Eof = false }, func(x *api.ReadResult) { x.Chunk.Data = nil; x.Chunk.Eof = false }} {
		x := good()
		mutate(&x)
		if validateRead(x, r, 0, 3) == nil {
			t.Fatal("invalid chunk accepted")
		}
	}
	if e := validateRead(api.ReadResult{Outcome: "changed"}, r, 0, 3); e != nil {
		t.Fatal(e)
	}
}
