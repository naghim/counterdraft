package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallCreatesMissingFolder(t *testing.T) {
	target := filepath.Join(t.TempDir(), "game", "dota", "cfg")
	path, err := Install("http://127.0.0.1:4000/", target)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if filepath.Base(path) != Filename {
		t.Errorf("wrote %q, want filename %q", path, Filename)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(data), "http://127.0.0.1:4000/") {
		t.Error("written config does not contain the listener URI")
	}
}

func TestDetectAndInstallIntoFakeSteamLibrary(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("steam root discovery is linux-specific here")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, ".steam", "steam", "steamapps", "common", "dota 2 beta", "game", "dota", "cfg")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cands := Candidates()
	want := filepath.Join(cfgDir, gsiFolderName)
	if len(cands) == 0 || cands[0] != want {
		t.Fatalf("Candidates = %v, want first %q", cands, want)
	}

	path, err := Install("http://127.0.0.1:4000/", "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if path != filepath.Join(want, Filename) {
		t.Errorf("Install path = %q, want %q", path, filepath.Join(want, Filename))
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config not written: %v", err)
	}
}

func TestContentPointsAtURI(t *testing.T) {
	got := Content("http://127.0.0.1:4000/")
	for _, want := range []string{
		`"uri"       "http://127.0.0.1:4000/"`,
		`"draft"     "1"`,
		`"players"   "1"`,
		`"provider"  "1"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config content missing %q", want)
		}
	}
}

func TestExpandOverride(t *testing.T) {
	cases := map[string]string{
		"/games/dota 2 beta/game/dota/cfg":                       "/games/dota 2 beta/game/dota/cfg",
		"/games/dota 2 beta/game/dota/cfg/gamestate_integration": "/games/dota 2 beta/game/dota/cfg/gamestate_integration",
		"/games/dota 2 beta/game/dota":                           "/games/dota 2 beta/game/dota/cfg/gamestate_integration",
		"/srv/steam/dota":                                        "/srv/steam/dota/cfg/gamestate_integration",
	}
	for in, want := range cases {
		if got := expandOverride(in); got != want {
			t.Errorf("expandOverride(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFilenameIsValidForGSI(t *testing.T) {
	if !strings.HasPrefix(Filename, "gamestate_integration_") || !strings.HasSuffix(Filename, ".cfg") {
		t.Fatalf("GSI requires filenames matching gamestate_integration_*.cfg, got %q", Filename)
	}
}
