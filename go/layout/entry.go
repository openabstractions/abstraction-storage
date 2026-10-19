package layout

import "slices"

// Holder is who an entry's owner or one of its writers or readers is, as
// STO-L4 and STO-L9 judge them.
type Holder string

const (
	// Self is this account.
	Self Holder = "self"
	// Admin is root on macOS and Linux; SYSTEM, Administrators or
	// TrustedInstaller on Windows.
	Admin Holder = "admin"
	// Other is any other account or group, Everyone and Users included.
	Other Holder = "other"
)

// Entry is what the file system says about one folder or file: whether it
// exists, whether it is a link, who owns it, and who besides its owner may
// write or read it. Inspect fills it for a real path; the scenario corpus
// states it.
type Entry struct {
	Path   string `json:"path,omitempty"`
	Exists bool   `json:"exists"`
	// Link is a symbolic link or, on Windows, any reparse point.
	Link bool `json:"link,omitempty"`
	Dir  bool `json:"dir,omitempty"`
	// Owner is who owns the entry: Admin for root, SYSTEM, Administrators
	// or TrustedInstaller whoever runs, otherwise Self or Other. Mine
	// reports that the owner is this process's own account, root included
	// when root runs. OwnerName names the owner for a message.
	Owner     Holder `json:"owner,omitempty"`
	Mine      bool   `json:"mine,omitempty"`
	OwnerName string `json:"owner_name,omitempty"`
	// Writers is who may change or remove the entry: on POSIX the owner
	// when its write bit is set, and Other for a group or other write bit;
	// on Windows each allowed entry of the DACL by its holder.
	Writers []Holder `json:"writers,omitempty"`
	// Readers is who may read or list the entry without writing it.
	Readers []Holder `json:"readers,omitempty"`
	// Private reports that the folder already is a folder private to this
	// account as identity CONTRACT.md ID-D1 defines one, the check
	// privatedir.CheckDir applies: owned by this account with a protected
	// DACL of this account and SYSTEM on Windows, mode 0700 elsewhere.
	Private bool `json:"private,omitempty"`
}

// Action is what Ensure does with a folder after judging it (STO-L9).
type Action string

const (
	// Create makes a folder that does not exist, private to this account
	// (privatedir.Make).
	Create Action = "create"
	// Keep leaves a folder that is already private to this account as it
	// is.
	Keep Action = "keep"
	// Tighten gives a folder no other account can write the private
	// descriptor (privatedir.Tighten): mode 0700 on POSIX; on Windows this
	// account as owner and the protected DACL of this account and SYSTEM.
	Tighten Action = "tighten"
)

// JudgeOwnerOnly is STO-L9 on one folder of a user or isolated root. The
// checks run in this order and the first that holds decides: a path that is
// not absolute is refused (not_absolute); a missing folder is created; a
// link (link), a file (not_directory), a folder this account does not own
// (not_owner) and a folder another account can write (open_to_others) are
// refused; a folder already private to this account (identity ID-D1) that
// no other account can read is kept; any other folder is tightened. This
// account owns the folder when Mine is set, which Owner Self implies. On
// Windows a folder Administrators or SYSTEM own is tightened as well: an
// elevated process of the same person owns what it creates as
// Administrators, and only an administrator could have made it. Admin among
// the writers never refuses, since an administrator can write every folder.
func JudgeOwnerOnly(goos string, e Entry) (Action, *Refusal) {
	switch {
	case !isAbs(goos, e.Path):
		return "", refuse(NotAbsolute, e.Path, "a root or a folder under it is an absolute path")
	case !e.Exists:
		return Create, nil
	case e.Link:
		return "", refuse(Link, e.Path, "a root is never reached through a link or junction")
	case !e.Dir:
		return "", refuse(NotDirectory, e.Path, "a root is a folder")
	case !e.Mine && e.Owner != Self && !(e.Owner == Admin && goos == "windows"):
		return "", refuse(NotOwner, e.Path, "it is owned by %s, not by this account", ownerName(e))
	case slices.Contains(e.Writers, Other):
		return "", refuse(OpenToOthers, e.Path, "another account can write it, so its contents are not this account's")
	case !e.Private || slices.Contains(e.Readers, Other):
		return Tighten, nil
	}
	return Keep, nil
}

// JudgeMachine is STO-L4 on a machine file and every folder from it up to
// the machine root, the file first: each must exist, be owned by an
// administrator and be writable by administrators only. Every entry that
// exists is judged first, in chain order, and the first one owned or
// writable by anyone else gives machine_untrusted; only then does a missing
// entry, or an empty chain, give machine_absent. Each entry is the object a
// reader opens, links followed, so Link is not read; Dir and Readers are not
// read either. This account (Self) is never an administrator, even in an
// elevated process, whose files Windows gives to Administrators. A Windows
// folder an ordinary account may add new entries to still passes, since
// every file read is checked itself; one it may delete from or re-permission
// does not.
func JudgeMachine(chain []Entry) *Refusal {
	var missing *Entry
	for i, e := range chain {
		switch {
		case !e.Exists:
			if missing == nil {
				missing = &chain[i]
			}
		case e.Owner != Admin:
			return refuse(MachineUntrusted, e.Path, "it is owned by %s, not by an administrator, so no administrator wrote it", ownerName(e))
		case slices.Contains(e.Writers, Other), slices.Contains(e.Writers, Self):
			return refuse(MachineUntrusted, e.Path, "an account other than an administrator can write it, so anyone could have written it")
		}
	}
	if len(chain) == 0 {
		return refuse(MachineAbsent, "", "no machine file was named")
	}
	if missing != nil {
		return refuse(MachineAbsent, missing.Path, "it does not exist")
	}
	return nil
}

func ownerName(e Entry) string {
	if e.OwnerName != "" {
		return e.OwnerName
	}
	return string(e.Owner)
}
