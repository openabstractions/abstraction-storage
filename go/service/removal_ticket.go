package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	contentdigest "github.com/openabstractions/abstraction-storage/go/digest"
)

// ErrInlineRemovalRefused distinguishes an invalid or consumed answer from
// inability to persist authoritative Asks state.
var ErrInlineRemovalRefused = errors.New("storage: inline removal answer refused")

type removalTicket struct {
	digest, observation, occurrence string
	expires                         int64
}

const removalTicketLifetime = 2 * time.Minute

func (c removalReceiver) ticketScope() string {
	binding := nativeBindingOf(c.ctx)
	if binding == nil || !binding.owner.valid(c.scope) {
		return ""
	}
	return c.scope
}

func (c removalReceiver) issueTicket(digest, observation string) (string, error) {
	scope := c.ticketScope()
	if scope == "" {
		return "", ErrInlineRemovalRefused
	}
	r := c.removal
	var occurrence [16]byte
	if _, err := rand.Read(occurrence[:]); err != nil {
		return "", err
	}
	body := strings.Join([]string{digest, observation, hex.EncodeToString(occurrence[:]),
		strconv.FormatInt(r.now().Add(removalTicketLifetime).UnixNano(), 10)}, ".")
	return body + "." + hex.EncodeToString(r.signTicket("remove\x00"+scope+"\x00"+body)), nil
}

func (r *removal) signTicket(body string) []byte {
	mac := hmac.New(sha256.New, r.seal[:])
	mac.Write([]byte(r.store.Name() + "\x00" + body))
	return mac.Sum(nil)
}

// RemovalTicketDigest checks the private Panel transport's bounds and syntax.
// Only the runtime instance that issued the ticket can authenticate it.
func RemovalTicketDigest(raw string) (string, bool) {
	ticket, _, _, ok := parseRemovalTicket(raw)
	return ticket.digest, ok
}

func parseRemovalTicket(raw string) (removalTicket, string, []byte, bool) {
	if len(raw) > 256 {
		return removalTicket{}, "", nil, false
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 5 || !contentdigest.Valid(parts[0]) || len(parts[1]) != 64 || len(parts[2]) != 32 || len(parts[4]) != 64 {
		return removalTicket{}, "", nil, false
	}
	observation, observationErr := hex.DecodeString(parts[1])
	occurrence, occurrenceErr := hex.DecodeString(parts[2])
	mac, macErr := hex.DecodeString(parts[4])
	expires, expiresErr := strconv.ParseInt(parts[3], 10, 64)
	if observationErr != nil || occurrenceErr != nil || macErr != nil || expiresErr != nil || expires <= 0 ||
		hex.EncodeToString(observation) != parts[1] || hex.EncodeToString(occurrence) != parts[2] ||
		hex.EncodeToString(mac) != parts[4] || strconv.FormatInt(expires, 10) != parts[3] {
		return removalTicket{}, "", nil, false
	}
	return removalTicket{parts[0], parts[1], parts[2], expires}, raw[:strings.LastIndexByte(raw, '.')], mac, true
}

func (c removalReceiver) openTicket(raw string) (removalTicket, bool) {
	scope := c.ticketScope()
	ticket, body, mac, ok := parseRemovalTicket(raw)
	r := c.removal
	return ticket, scope != "" && ok && !ticket.expired(r.now()) &&
		hmac.Equal(mac, r.signTicket("remove\x00"+scope+"\x00"+body))
}

func (t removalTicket) expired(now time.Time) bool { return now.UnixNano() >= t.expires }
