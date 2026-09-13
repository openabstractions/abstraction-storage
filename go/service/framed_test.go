package service

import (
	"context"
	"errors"
	"fmt"
	identity "github.com/openabstractions/abstraction-identity"
	storage "github.com/openabstractions/abstraction-storage/go"
	client "github.com/openabstractions/abstraction-storage/go/client"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFramedStorageAndInstalledCPP(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("current Program proof limitation")
	}
	dir := t.TempDir()
	digest := "sha256:" + strings.Repeat("a", 64)
	if e := os.WriteFile(filepath.Join(dir, strings.Repeat("a", 64)), []byte("content fixture bytes"), 0600); e != nil {
		t.Fatal(e)
	}
	store := storage.NewForeignStore("fixture", dir, "")
	endpoint := filepath.Join(dir, "service.sock")
	if runtime.GOOS == "windows" {
		endpoint = fmt.Sprintf(`\\.\pipe\oa-storage-%d`, time.Now().UnixNano())
	}
	var revoked atomic.Bool
	policy := func(ctx context.Context, p *identity.Peer, d string) error {
		if p == nil || ctx.Err() != nil || d != digest || revoked.Load() {
			return errors.New("denied")
		}
		return nil
	}
	start := func() (*Host, func()) {
		h, e := Listen(endpoint, store, policy)
		if e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- h.Serve(ctx) }()
		return h, func() {
			cancel()
			h.Close()
			select {
			case e := <-done:
				if e != nil {
					t.Error(e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("service did not drain")
			}
		}
	}
	_, stop := start()
	defer func() {
		if stop != nil {
			stop()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := client.New(endpoint)
	opened, e := c.Open(ctx, digest)
	if e != nil || opened.Resource == nil {
		t.Fatal(opened, e)
	}
	read, e := c.Read(ctx, *opened.Resource, 0, 4)
	if e != nil || string(read.Chunk.Data) != "cont" {
		t.Fatal(read, e)
	}
	denied, e := c.Open(ctx, "sha256:"+strings.Repeat("b", 64))
	if e != nil || denied.Outcome != "forbidden" {
		t.Fatal(denied, e)
	}
	probe := os.Getenv("OA_CPP_STORAGE_PROBE")
	if probe == "" {
		t.Log("C++ consumer not supplied; native Go path tested")
		return
	}
	run := func(exe, mode string, args ...string) string {
		t.Helper()
		all := append([]string{endpoint, digest, mode}, args...)
		out, e := exec.CommandContext(ctx, exe, all...).CombinedOutput()
		if e != nil {
			t.Fatalf("%s %s: %v", mode, out, e)
		}
		return string(out)
	}
	t.Log(run(probe, "roundtrip"))
	resource := strings.Fields(run(probe, "open"))
	if len(resource) != 2 {
		t.Fatal(resource)
	}
	foreign := filepath.Join(dir, "foreign-consumer.exe")
	data, e := os.ReadFile(probe)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(foreign, data, 0700); e != nil {
		t.Fatal(e)
	}
	run(foreign, "foreign", resource...)
	revoked.Store(true)
	run(probe, "revoked", resource...)
	revoked.Store(false)
	resource = strings.Fields(run(probe, "open"))
	stop()
	stop = nil
	_, stop = start()
	run(probe, "gap", resource...)
}
