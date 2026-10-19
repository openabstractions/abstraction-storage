# Locations scenarios

`scenarios.json` holds the scenarios of the storage contract's
[Locations](../../CONTRACT.md#locations) section, and `rules.tsv` accounts
for every rule: the scenarios that show it, or why none can. An
implementation in any language reads the same file; the Go one is
`go/layout/corpus_test.go`.

Each scenario has a `name`, the `rules` it shows, a `call`, the operating
system's answers in `platform`, the call's `args` and the `expect`ed
result. Paths are spelled for the scenario's `os` (`windows`, `darwin` or
`linux`), whatever machine runs the scenario.

## Platform

| field | meaning |
|---|---|
| `os` | the operating system |
| `known` | Windows known folders by name (`LocalAppData`, `ProgramData`, `Pictures`, ...); a name that is absent cannot be read |
| `temp` | the Windows per-user temporary folder; absent means it cannot be read, which refuses the user location |
| `view` | the package family whose private profile copy a Windows process sees; absent or empty is the real view |
| `view_error` | the Windows profile view cannot be read, for this reason |
| `env` | `HOME`, the XDG variables and `TMPDIR` on macOS and Linux; an absent name is unset, and an empty or relative value counts as unset |
| `files` | file contents by path, such as the XDG `user-dirs.dirs` |
| `uid` | the account's user id on macOS and Linux |
| `sandbox` | the sandbox the process runs in (`app-sandbox`, `flatpak`, `snap`), or empty |

## Calls

| call | args | expect |
|---|---|---|
| `user` | none | `roots` for the user location |
| `isolated` | `root`: the isolated root | `roots` for the isolated location |
| `machine` | none | `machine`: `root`, `config_file`, `state` (the reserved machine service state), `old_config_file`, `upgrade_v1` (empty where the platform has none) |
| `classify` | `path` | `root`: the name of the longest root of the user location the path lies under (`data`, `runtime`, `config`, `cache`, `logs`, `tmp`, `short` or `sockets`; a child of `D` such as `engines` is `data`), or `""` when it lies under none or is relative; the path is cleaned first and compared by spelling, with no case folded |
| `path_bound` | `location` (`user` or `isolated`), `root` for isolated, `kind` (`s`, `e`, `p`, `m`), `elems`: folder names below the kind's folder | `path` |
| `person` | `folder` (`pictures`, `documents`, `downloads`, `music`, `videos`, `desktop`); `known` names the Windows folder with a capital, as `Pictures` | `path` |
| `owner_only` | `entry` | `action`: `create`, `keep` or `tighten`, judged in STO-L9's order |
| `trust` | `chain`: entries from a machine file up to its machine root, each the object a reader opens with links followed (`link`, `dir` and `readers` are not read) | `ok` |

Any call may instead expect `refusal`, the typed reason.

`roots` names `data`, `runtime`, `config`, `cache`, `logs`, `tmp`, `short`,
`sockets`, `install`, `upgrade_v1`, `config_file`, and the children
`provider_data` (provider `example`), `engines` (provider `example`, version
`1.0`), `models`, `setup` and `mcp_gateway`. A name that is absent is not
checked; an empty value means the location has no such root. `outside` lists
folders no root may lie in, and `not_child_of` names a folder no root may be
a direct child of (STO-L5).

An `entry` is what the file system says about one folder or file: `path`,
`exists`, `link`, `dir`, `owner` (`self`, `admin` or `other`), `mine` (the
owner is this process's account, which `owner: self` implies; root's own
folders are `admin` and `mine` to a process that runs as root), `writers`, the holders who may change or
remove it, `readers`, the holders who may only read it, and `private`: the
folder already is a folder private to this account as the identity
contract's `ID-D1` defines one (owned by the account with a protected DACL of
the account and SYSTEM on Windows, mode 0700 elsewhere).
