package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"

	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
)

const nativeScopeProfile = "owner-native-program@1"

// nativeOwner is captured only by the receiving host. Executable records the
// observation; account and canonical Program select new durable identities.
type nativeOwner struct {
	AccountKind string `json:"account_kind"`
	Account     string `json:"account"`
	Program     string `json:"program"`
	Executable  string `json:"executable"`
}

func (n nativeOwner) scope() string {
	raw, _ := json.Marshal([]string{nativeScopeProfile, n.AccountKind, n.Account, n.Program})
	digest := sha256.Sum256(raw)
	return nativeScopeProfile + ":" + hex.EncodeToString(digest[:])
}

func (n nativeOwner) valid(scope string) bool {
	account := n.AccountKind == "windows" && n.Account != ""
	if n.AccountKind == "posix" {
		uid, err := strconv.Atoi(n.Account)
		account = err == nil && uid >= 0 && strconv.Itoa(uid) == n.Account
	}
	return account && len(n.Account) <= 256 &&
		!strings.ContainsAny(n.Account, "\x00\r\n") && identity.ValidSubjectProgram(n.Program) &&
		filepath.IsAbs(n.Executable) && filepath.Clean(n.Executable) == n.Executable &&
		!strings.ContainsAny(n.Executable, "\x00\r\n") && len(n.Executable) <= 4096 && n.scope() == scope
}

func (n nativeOwner) sameProgram(other nativeOwner) bool {
	return n.AccountKind == other.AccountKind && n.Account == other.Account && n.Program == other.Program
}

type nativeBinding struct {
	owner  nativeOwner
	legacy string
}
type nativeBindingKey struct{}

func receivingNativeBinding(peer *identity.Peer, owner string) *nativeBinding {
	legacy, observed := callerCapture(peer, owner)
	// The framed listener retains its full Program check. This projection needs
	// the account and canonical program facts; it retains no process identity.
	subject, verdict := identity.AdmitOwner(peer, identity.Need{User: listen.Program.User, Path: listen.Program.Path}, owner)
	if legacy == "" || verdict != identity.OwnerAdmitted {
		return nil
	}
	native := nativeOwner{subject.AccountKind, subject.Account, subject.Program, observed}
	if !native.valid(native.scope()) {
		return nil
	}
	return &nativeBinding{owner: native, legacy: legacy}
}

func nativeBindingOf(ctx context.Context) *nativeBinding {
	if ctx == nil {
		return nil
	}
	binding, _ := ctx.Value(nativeBindingKey{}).(*nativeBinding)
	return binding
}

func ownsScope(ctx context.Context, scope, retained string) bool {
	if scope == "" {
		return false
	}
	if scope == retained {
		return true
	}
	binding := nativeBindingOf(ctx)
	return binding != nil && scope == binding.owner.scope() && retained == binding.legacy
}
