//go:build !windows

package service

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestFIFORefusesWithoutOpening(t *testing.T) {
	_, c, d, path := fixture(t)
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if e := syscall.Mkfifo(path, 0600); e != nil {
		t.Fatal(e)
	}
	done := make(chan string, 1)
	go func() { r, _ := c.Open(d); done <- r.Outcome }()
	select {
	case outcome := <-done:
		if outcome != "unsupported" {
			t.Fatal(outcome)
		}
	case <-time.After(time.Second):
		fd, _ := syscall.Open(path, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
		if fd >= 0 {
			syscall.Close(fd)
		}
		t.Fatal("FIFO open blocked")
	}
	if _, e := openRegular(filepath.Join(filepath.Dir(path), "absent")); e == nil {
		t.Fatal("absent opened")
	}
}
