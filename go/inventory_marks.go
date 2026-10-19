package storage

// The errors a storage inventory composition puts on a Store record when it
// could not read that store now. Each says the store was not read by this
// observation; a reader keeps what it last knew of the store for a bounded
// time and then treats the store as gone (the runtime's composition,
// serve/runtime_inventory.go).
const (
	// UnansweredSourceError is the whole error on each store of a source
	// that did not answer in time; the composition carries none of its
	// records.
	UnansweredSourceError = "the inventory source did not answer in time"
	// StaleObservationError begins the error on each store of a source that
	// answered from an earlier observation while a newer one still runs;
	// the RFC 3339 time that observation was taken follows it.
	StaleObservationError = "answered from an earlier observation, taken at "
)
