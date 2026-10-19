package layout

import (
	"errors"
	"strings"
	"testing"
)

func fakeEnv(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

// ST-2 ruling 2a: Windows keeps today's %LOCALAPPDATA% paths byte for byte;
// macOS and Linux keep durable state under the data root and the records log
// under the logs root, never under the cache folder.
func TestAccountDataTable(t *testing.T) {
	local := `C:\Users\person\AppData\Local`
	windowsLocal := func() (string, error) { return local, nil }
	for _, c := range []struct {
		name     string
		p        Platform
		data     string
		provider string
		engine   string
		log      string
		cache    string
	}{
		{"windows", Platform{GOOS: "windows"},
			local,
			`C:\Users\person\AppData\Local\OpenAbstractions\providers`,
			`C:\Users\person\AppData\Local\OpenAbstractions\engines\llama.cpp-b1`,
			`C:\Users\person\AppData\Local\openabstractions\logging\records.jsonl`, ""},
		{"darwin", Platform{GOOS: "darwin", Getenv: fakeEnv(map[string]string{"HOME": "/Users/person", "TMPDIR": "/var/folders/x/T/"})},
			"/Users/person/Library/Application Support/openabstractions",
			"/Users/person/Library/Application Support/openabstractions/OpenAbstractions/providers",
			"/Users/person/Library/Application Support/openabstractions/OpenAbstractions/engines/llama.cpp-b1",
			"/Users/person/Library/Logs/openabstractions/records.jsonl", "/Users/person/Library/Caches"},
		{"linux", Platform{GOOS: "linux", UID: 1000, Getenv: fakeEnv(map[string]string{"HOME": "/home/person", "XDG_DATA_HOME": "/data/person", "XDG_STATE_HOME": "/state/person", "XDG_CACHE_HOME": "/cache/person"})},
			"/data/person/openabstractions",
			"/data/person/openabstractions/OpenAbstractions/providers",
			"/data/person/openabstractions/OpenAbstractions/engines/llama.cpp-b1",
			"/state/person/log/openabstractions/records.jsonl", "/cache/person"},
		{"linux defaults", Platform{GOOS: "linux", UID: 1000, Getenv: fakeEnv(map[string]string{"HOME": "/home/person"})},
			"/home/person/.local/share/openabstractions",
			"/home/person/.local/share/openabstractions/OpenAbstractions/providers",
			"/home/person/.local/share/openabstractions/OpenAbstractions/engines/llama.cpp-b1",
			"/home/person/.local/state/log/openabstractions/records.jsonl", "/home/person/.cache"},
	} {
		t.Run(c.name, func(t *testing.T) {
			data, err := accountData(c.p, windowsLocal)
			if err != nil || data != c.data {
				t.Fatalf("AccountData = %q, %v; want %q", data, err, c.data)
			}
			provider, err := providerStateRoot(c.p, windowsLocal)
			if err != nil || provider != c.provider {
				t.Fatalf("provider state root = %q, %v; want %q", provider, err, c.provider)
			}
			engine, err := engineDir(c.p, windowsLocal, "llama.cpp", "b1")
			if err != nil || engine != c.engine {
				t.Fatalf("engine dir = %q, %v; want %q", engine, err, c.engine)
			}
			log, err := recordsLog(c.p, windowsLocal)
			if err != nil || log != c.log {
				t.Fatalf("records log = %q, %v; want %q", log, err, c.log)
			}
			if c.cache != "" {
				for _, got := range []string{data, provider, engine, log} {
					if strings.HasPrefix(got, c.cache) {
						t.Fatalf("%s lies under the cache folder %s", got, c.cache)
					}
				}
			}
		})
	}
}

func TestAccountDataRefusesWithoutAnAbsoluteHome(t *testing.T) {
	windowsLocal := func() (string, error) { return "", errors.New("unused") }
	for _, goos := range []string{"darwin", "linux"} {
		for _, home := range []string{"", "relative/home"} {
			p := Platform{GOOS: goos, Getenv: fakeEnv(map[string]string{"HOME": home, "XDG_DATA_HOME": "/data", "XDG_STATE_HOME": "/state"})}
			if _, err := accountData(p, windowsLocal); ReasonOf(err) != NoProfileFolder {
				t.Fatalf("%s HOME %q: AccountData %v, want %s", goos, home, err, NoProfileFolder)
			}
			if _, err := recordsLog(p, windowsLocal); ReasonOf(err) != NoProfileFolder {
				t.Fatalf("%s HOME %q: RecordsLog %v, want %s", goos, home, err, NoProfileFolder)
			}
			if _, err := localAppData(p); ReasonOf(err) != NoProfileFolder {
				t.Fatalf("%s HOME %q: LocalAppData %v, want %s", goos, home, err, NoProfileFolder)
			}
		}
	}
	// Windows keeps os.UserCacheDir's own error.
	want := errors.New("%LOCALAPPDATA% is not defined")
	if _, err := accountData(Platform{GOOS: "windows"}, func() (string, error) { return "", want }); err != want {
		t.Fatalf("windows error = %v, want %v", err, want)
	}
}

func TestLocalAppDataPerPlatform(t *testing.T) {
	for _, c := range []struct {
		name string
		p    Platform
		want string
	}{
		{"windows", Platform{GOOS: "windows", Known: func(name string) (string, error) {
			if name != "LocalAppData" {
				return "", errors.New("unexpected known folder " + name)
			}
			return `C:\Users\person\AppData\Local`, nil
		}}, `C:\Users\person\AppData\Local`},
		{"darwin", Platform{GOOS: "darwin", Getenv: fakeEnv(map[string]string{"HOME": "/Users/person"})}, "/Users/person/Library/Application Support"},
		{"linux", Platform{GOOS: "linux", Getenv: fakeEnv(map[string]string{"HOME": "/home/person", "XDG_DATA_HOME": "/data/person"})}, "/data/person"},
		{"linux default", Platform{GOOS: "linux", Getenv: fakeEnv(map[string]string{"HOME": "/home/person"})}, "/home/person/.local/share"},
	} {
		if got, err := localAppData(c.p); err != nil || got != c.want {
			t.Errorf("%s: LocalAppData = %q, %v; want %q", c.name, got, err, c.want)
		}
	}
}
