// Package config installs the Dota 2 Game State Integration cfg file. GSI is an
// official Valve feature: the game reads a JSON cfg describing a local URI and
// POSTs visible game state to it. Nothing here touches the game process.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Filename is the GSI cfg filename. Dota only loads files matching
// gamestate_integration_*.cfg.
const Filename = "gamestate_integration_counterdraft.cfg"

const (
	dotaCfgSubPath = "steamapps/common/dota 2 beta/game/dota/cfg"
	gsiFolderName  = "gamestate_integration"
)

// Content renders the GSI cfg pointing at uri.
func Content(uri string) string {
	uri = strings.TrimSpace(uri)
	return fmt.Sprintf(`"counterdraft"
{
    "uri"       "%s"
    "timeout"   "5.0"
    "buffer"    "0.1"
    "throttle"  "0.1"
    "heartbeat" "10.0"
    "data"
    {
        "provider"  "1"
        "map"       "1"
        "player"    "1"
        "hero"      "1"
        "abilities" "1"
        "items"     "1"
        "draft"     "1"
        "players"   "1"
        "wearables" "0"
    }
}
`, uri)
}

// steamRoots lists likely Steam install roots for the current OS.
func steamRoots() []string {
	var roots []string
	addHome := func(parts ...string) {
		if home, err := os.UserHomeDir(); err == nil {
			roots = append(roots, filepath.Join(append([]string{home}, parts...)...))
		}
	}
	switch runtime.GOOS {
	case "windows":
		if pf := os.Getenv("ProgramFiles(x86)"); pf != "" {
			roots = append(roots, filepath.Join(pf, "Steam"))
		}
		if pf := os.Getenv("ProgramFiles"); pf != "" {
			roots = append(roots, filepath.Join(pf, "Steam"))
		}
		roots = append(roots, `C:\Program Files (x86)\Steam`, `C:\Program Files\Steam`)
	case "darwin":
		addHome("Library", "Application Support", "Steam")
	default:
		addHome(".steam", "steam")
		addHome(".steam", "root")
		addHome(".local", "share", "Steam")
		addHome(".var", "app", "com.valvesoftware.Steam", ".local", "share", "Steam")
	}
	return roots
}

// libraryFolders returns every Steam library path rooted at root, including
// root itself, by parsing steamapps/libraryfolders.vdf.
func libraryFolders(root string) []string {
	libs := []string{root}
	vdf := filepath.Join(root, "steamapps", "libraryfolders.vdf")
	data, err := os.ReadFile(vdf)
	if err != nil {
		return libs
	}
	for _, line := range strings.Split(string(data), "\n") {
		idx := strings.Index(line, `"path"`)
		if idx < 0 {
			continue
		}
		rest := line[idx+len(`"path"`):]
		if p, ok := firstQuoted(rest); ok {
			libs = append(libs, filepath.Clean(p))
		}
	}
	return libs
}

func firstQuoted(s string) (string, bool) {
	start := strings.IndexByte(s, '"')
	if start < 0 {
		return "", false
	}
	end := strings.IndexByte(s[start+1:], '"')
	if end < 0 {
		return "", false
	}
	return s[start+1 : start+1+end], true
}

// Candidates returns every GSI cfg directory we can install into. Detection
// keys off the Dota cfg folder, which exists on every install; the
// gamestate_integration subfolder is created on demand.
func Candidates() []string {
	var out []string
	seen := map[string]bool{}
	for _, root := range steamRoots() {
		for _, lib := range libraryFolders(root) {
			cfg := filepath.Join(lib, filepath.FromSlash(dotaCfgSubPath))
			if fi, err := os.Stat(cfg); err != nil || !fi.IsDir() {
				continue
			}
			gsi := filepath.Join(cfg, gsiFolderName)
			if seen[gsi] {
				continue
			}
			seen[gsi] = true
			out = append(out, gsi)
		}
	}
	return out
}

// FindCfgDir returns the first existing GSI cfg directory.
func FindCfgDir() (string, bool) {
	c := Candidates()
	if len(c) == 0 {
		return "", false
	}
	return c[0], true
}

// Install writes the GSI cfg into override (if non-empty) or the first detected
// Dota cfg directory, creating the gamestate_integration folder if needed. It
// returns the written path.
func Install(uri, override string) (string, error) {
	var dir string
	if strings.TrimSpace(override) != "" {
		dir = expandOverride(override)
	} else {
		found, ok := FindCfgDir()
		if !ok {
			return "", fmt.Errorf("config: could not find Dota 2 install; set the path manually")
		}
		dir = found
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("config: create %s: %w", dir, err)
	}
	path := filepath.Join(dir, Filename)
	if err := os.WriteFile(path, []byte(Content(uri)), 0o644); err != nil {
		return "", fmt.Errorf("config: write %s: %w", path, err)
	}
	return path, nil
}

// expandOverride normalises a user-supplied path so it always points at the
// gamestate_integration folder. It accepts that folder itself, the Dota cfg
// folder, or the game/dota folder.
func expandOverride(raw string) string {
	p := filepath.Clean(strings.TrimSpace(raw))
	switch filepath.Base(p) {
	case gsiFolderName, "cfg":
		return p
	case "dota":
		return filepath.Join(p, "cfg", gsiFolderName)
	}
	if fi, err := os.Stat(filepath.Join(p, "cfg")); err == nil && fi.IsDir() {
		return filepath.Join(p, "cfg", gsiFolderName)
	}
	if fi, err := os.Stat(filepath.Join(p, gsiFolderName)); err == nil && fi.IsDir() {
		return p
	}
	return filepath.Join(p, gsiFolderName)
}
