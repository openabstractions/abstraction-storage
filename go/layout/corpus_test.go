package layout

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The Locations scenarios are language-neutral: any implementation of the
// contract section reads abstraction-storage/testdata/locations and must give
// these answers. This driver is the Go one.

type scenarioFile struct {
	About     string     `json:"about"`
	Scenarios []scenario `json:"scenarios"`
}

type scenario struct {
	Name     string          `json:"name"`
	Rules    []string        `json:"rules"`
	Call     string          `json:"call"`
	Platform scenarioOS      `json:"platform"`
	Args     scenarioArgs    `json:"args"`
	Expect   json.RawMessage `json:"expect"`
}

type scenarioOS struct {
	OS        string            `json:"os"`
	Known     map[string]string `json:"known"`
	Temp      *string           `json:"temp"`
	View      string            `json:"view"`
	ViewError string            `json:"view_error"`
	Env       map[string]string `json:"env"`
	Files     map[string]string `json:"files"`
	UID       int               `json:"uid"`
	Sandbox   string            `json:"sandbox"`
}

type scenarioArgs struct {
	Root     string   `json:"root"`
	Path     string   `json:"path"`
	Location string   `json:"location"`
	Kind     string   `json:"kind"`
	Elems    []string `json:"elems"`
	Folder   string   `json:"folder"`
	Entry    Entry    `json:"entry"`
	Chain    []Entry  `json:"chain"`
}

type scenarioExpect struct {
	Refusal    Reason            `json:"refusal"`
	Roots      map[string]string `json:"roots"`
	Outside    []string          `json:"outside"`
	NotChildOf string            `json:"not_child_of"`
	Machine    map[string]string `json:"machine"`
	Root       *string           `json:"root"`
	Path       string            `json:"path"`
	Action     Action            `json:"action"`
	OK         bool              `json:"ok"`
}

func (s scenarioOS) platform() Platform {
	p := Platform{GOOS: s.OS, UID: s.UID, Sandbox: s.Sandbox}
	p.Known = func(name string) (string, error) {
		if v, ok := s.Known[name]; ok {
			return v, nil
		}
		return "", fmt.Errorf("the scenario has no known folder %s", name)
	}
	p.Temp = func() (string, error) {
		if s.Temp == nil {
			return "", errors.New("the scenario has no temporary folder")
		}
		return *s.Temp, nil
	}
	p.Profile = func() (string, error) {
		if s.ViewError != "" {
			return "", errors.New(s.ViewError)
		}
		return s.View, nil
	}
	p.Getenv = func(name string) string { return s.Env[name] }
	p.ReadFile = func(name string) ([]byte, error) {
		if v, ok := s.Files[name]; ok {
			return []byte(v), nil
		}
		return nil, fs.ErrNotExist
	}
	return p
}

var kinds = map[string]Kind{"s": EngineSockets, "e": GPUEngines, "p": GPUProviderState, "m": ModelStages}

func corpusDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "locations")
}

func loadScenarios(t *testing.T) []scenario {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(corpusDir(t), "scenarios.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f scenarioFile
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		t.Fatalf("scenarios.json: %v", err)
	}
	return f.Scenarios
}

func TestLocationsScenarios(t *testing.T) {
	for _, sc := range loadScenarios(t) {
		t.Run(sc.Name, func(t *testing.T) {
			var want scenarioExpect
			dec := json.NewDecoder(strings.NewReader(string(sc.Expect)))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&want); err != nil {
				t.Fatalf("expect: %v", err)
			}
			got, err := run(sc)
			if want.Refusal != "" {
				if reason := ReasonOf(err); reason != want.Refusal {
					t.Fatalf("%v: refusal %q, want %q (err %v)", sc.Rules, reason, want.Refusal, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("%v: %v", sc.Rules, err)
			}
			check(t, sc, want, got)
		})
	}
}

// result is what a call gave, in the scenario's own words.
type result struct {
	roots   map[string]string
	machine map[string]string
	root    string
	path    string
	action  Action
}

func run(sc scenario) (result, error) {
	p := sc.Platform.platform()
	switch sc.Call {
	case "user", "classify":
		r, err := Resolve(p)
		if err != nil {
			return result{}, err
		}
		return result{roots: rootsOf(r), root: r.Classify(sc.Args.Path)}, nil
	case "isolated":
		r, err := ResolveIsolated(sc.Platform.OS, sc.Args.Root)
		if err != nil {
			return result{}, err
		}
		return result{roots: rootsOf(r)}, nil
	case "machine":
		m, err := ResolveMachine(p)
		if err != nil {
			return result{}, err
		}
		return result{machine: map[string]string{"root": m.Root, "config_file": m.ConfigFile, "state": m.State,
			"old_config_file": m.OldConfigFile, "upgrade_v1": m.UpgradeV1}}, nil
	case "path_bound":
		var r Roots
		var err error
		if sc.Args.Location == "isolated" {
			r, err = ResolveIsolated(sc.Platform.OS, sc.Args.Root)
		} else {
			r, err = Resolve(p)
		}
		if err != nil {
			return result{}, err
		}
		k, ok := kinds[sc.Args.Kind]
		if !ok {
			return result{}, fmt.Errorf("no kind %q", sc.Args.Kind)
		}
		got, err := r.PathBound(k, sc.Args.Elems...)
		return result{path: got}, err
	case "person":
		got, err := ResolvePerson(p, PersonFolder(sc.Args.Folder))
		return result{path: got}, err
	case "owner_only":
		action, refusal := JudgeOwnerOnly(sc.Platform.OS, sc.Args.Entry)
		if refusal != nil {
			return result{}, refusal
		}
		return result{action: action}, nil
	case "trust":
		if refusal := JudgeMachine(sc.Args.Chain); refusal != nil {
			return result{}, refusal
		}
		return result{}, nil
	}
	return result{}, fmt.Errorf("no call %q", sc.Call)
}

func rootsOf(r Roots) map[string]string {
	return map[string]string{
		"data": r.Data, "runtime": r.Runtime, "config": r.Config, "cache": r.Cache, "logs": r.Logs,
		"tmp": r.Tmp, "short": r.Short, "sockets": r.Sockets, "install": r.Install, "upgrade_v1": r.UpgradeV1,
		"config_file": r.ConfigFile(), "provider_data": r.ProviderData("example"), "engines": r.Engines("example", "1.0"),
		"models": r.Models(), "setup": r.Setup(), "mcp_gateway": r.MCPGateway(),
	}
}

func check(t *testing.T, sc scenario, want scenarioExpect, got result) {
	t.Helper()
	for name, path := range want.Roots {
		if got.roots[name] != path {
			t.Errorf("%v: %s = %q, want %q", sc.Rules, name, got.roots[name], path)
		}
	}
	for name, path := range want.Machine {
		if got.machine[name] != path {
			t.Errorf("%v: %s = %q, want %q", sc.Rules, name, got.machine[name], path)
		}
	}
	goos := sc.Platform.OS
	for name, path := range got.roots {
		if path == "" {
			continue
		}
		for _, banned := range want.Outside {
			if under(goos, path, banned) {
				t.Errorf("%v: %s %s lies in %s", sc.Rules, name, path, banned)
			}
		}
		if want.NotChildOf != "" && parentOf(goos, path) == join(goos, want.NotChildOf) {
			t.Errorf("%v: %s %s is a direct child of %s", sc.Rules, name, path, want.NotChildOf)
		}
	}
	if want.Root != nil && got.root != *want.Root {
		t.Errorf("%v: classified under %q, want %q", sc.Rules, got.root, *want.Root)
	}
	if want.Path != "" && got.path != want.Path {
		t.Errorf("%v: path %q, want %q", sc.Rules, got.path, want.Path)
	}
	if want.Action != "" && got.action != want.Action {
		t.Errorf("%v: action %q, want %q", sc.Rules, got.action, want.Action)
	}
}

func parentOf(goos, p string) string {
	sep := "/"
	if goos == "windows" {
		sep = `\`
	}
	i := strings.LastIndex(p, sep)
	if i <= 0 {
		return p
	}
	return join(goos, p[:i])
}

// Every rule is accounted for in rules.tsv, every scenario a rule names
// exists, and every scenario names a rule that names it back.
func TestLocationsRulesAreAccounted(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(corpusDir(t), "rules.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]scenario{}
	for _, sc := range loadScenarios(t) {
		if _, dup := byName[sc.Name]; dup {
			t.Errorf("two scenarios are called %s", sc.Name)
		}
		byName[sc.Name] = sc
	}
	listed := map[string][]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, ">") || strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 3 {
			t.Fatalf("rules.tsv wants three tab-separated fields: %q", line)
		}
		if strings.HasPrefix(f[2], "untestable: ") || strings.HasPrefix(f[2], "not built: ") {
			listed[f[0]] = nil
			continue
		}
		listed[f[0]] = strings.Fields(f[2])
	}
	for i := 1; i <= 9; i++ {
		tag := fmt.Sprintf("STO-L%d", i)
		names, ok := listed[tag]
		if !ok {
			t.Errorf("rules.tsv does not account for %s", tag)
		}
		for _, name := range names {
			sc, ok := byName[name]
			if !ok {
				t.Errorf("%s names a missing scenario %s", tag, name)
				continue
			}
			if !slices.Contains(sc.Rules, tag) {
				t.Errorf("%s names %s, which does not name it back", tag, name)
			}
		}
	}
	for _, sc := range byName {
		for _, tag := range sc.Rules {
			if !slices.Contains(listed[tag], sc.Name) {
				t.Errorf("scenario %s names %s, whose row does not list it", sc.Name, tag)
			}
		}
	}
}
