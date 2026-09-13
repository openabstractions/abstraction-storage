package service

import (
	"fmt"
	"testing"
)

func TestGlobalLimitAndRestartGap(t *testing.T) {
	r, c, d, _ := fixture(t)
	var first string
	for i := 0; i < MaxResources; i++ {
		other := c
		other.scope = fmt.Sprint(i)
		opened, _ := other.Open(d)
		if opened.Outcome != "opened" {
			t.Fatal(opened)
		}
		if i == 0 {
			first = opened.Resource.Handle
		}
	}
	x, _ := c.Open(d)
	if x.Outcome != "exhausted" {
		t.Fatal(x)
	}
	fresh := newRegistry(r.store, r.policy)
	defer fresh.close()
	restarted := receiver{fresh, "0", nil, c.ctx}
	gap, _ := restarted.Read(first, 0, 1)
	if gap.Outcome != "gap" {
		t.Fatal(gap)
	}
}
