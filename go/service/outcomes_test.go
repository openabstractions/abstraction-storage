package service

import "testing"

func TestOutcomeParsersRejectUnknownWords(t *testing.T) {
	if changePageOutcome("invented") != 0 || listingOutcome("invented") != 0 || beginOutcome("invented") != 0 || appendOutcome("invented") != 0 || commitOutcome("invented") != 0 || abortOutcome("invented") != 0 || openOutcome("invented") != 0 || readOutcome("invented") != 0 || closeOutcome("invented") != 0 {
		t.Fatal("unknown internal outcome mapped to a valid wire outcome")
	}
}
