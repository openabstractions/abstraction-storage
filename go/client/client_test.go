package client

import (
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
	"testing"
)

func TestChunkCombinations(t *testing.T) {
	r := Resource{Size: 3}
	good := func() api.ReadResult {
		return api.ReadResult{Outcome: api.ReadOutcomeData, Chunk: &api.Chunk{Offset: 0, Total: 3, Data: []byte("abc"), EOF: true}}
	}
	if e := validateRead(good(), r, 0, 3); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*api.ReadResult){func(x *api.ReadResult) { x.Outcome = api.ReadOutcomeGap }, func(x *api.ReadResult) { x.Chunk = nil }, func(x *api.ReadResult) { x.Chunk.Offset = 1 }, func(x *api.ReadResult) { x.Chunk.Total = 4 }, func(x *api.ReadResult) { x.Chunk.EOF = false }, func(x *api.ReadResult) { x.Chunk.Data = nil; x.Chunk.EOF = false }} {
		x := good()
		mutate(&x)
		if validateRead(x, r, 0, 3) == nil {
			t.Fatal("invalid chunk accepted")
		}
	}
	if e := validateRead(api.ReadResult{Outcome: api.ReadOutcomeChanged}, r, 0, 3); e != nil {
		t.Fatal(e)
	}
}
