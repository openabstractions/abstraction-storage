package service

import api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"

func changePageOutcome(word string) api.ChangePageOutcome {
	if outcome, ok := api.ParseChangePageOutcome(word); ok {
		return outcome
	}
	return 0
}

func listingOutcome(word string) api.ListingOutcome {
	if outcome, ok := api.ParseListingOutcome(word); ok {
		return outcome
	}
	return 0
}

func beginOutcome(word string) api.BeginOutcome {
	if outcome, ok := api.ParseBeginOutcome(word); ok {
		return outcome
	}
	return 0
}

func appendOutcome(word string) api.AppendOutcome {
	if outcome, ok := api.ParseAppendOutcome(word); ok {
		return outcome
	}
	return 0
}

func commitOutcome(word string) api.CommitOutcome {
	if outcome, ok := api.ParseCommitOutcome(word); ok {
		return outcome
	}
	return 0
}

func abortOutcome(word string) api.AbortOutcome {
	if outcome, ok := api.ParseAbortOutcome(word); ok {
		return outcome
	}
	return 0
}

func openOutcome(word string) api.OpenOutcome {
	if outcome, ok := api.ParseOpenOutcome(word); ok {
		return outcome
	}
	return 0
}

func readOutcome(word string) api.ReadOutcome {
	if outcome, ok := api.ParseReadOutcome(word); ok {
		return outcome
	}
	return 0
}

func closeOutcome(word string) api.CloseOutcome {
	if outcome, ok := api.ParseCloseOutcome(word); ok {
		return outcome
	}
	return 0
}
