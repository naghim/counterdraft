package vision

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

// appName is the directory under the user's data home that vision owns.
const appName = "counterdraft"

// dataDirEnv overrides the data directory. It exists so tests can point the
// portrait store at a temporary directory, exactly as DOTA_DRAFTER_DATA does in
// the Python original.
const dataDirEnv = "COUNTERDRAFT_DATA"

// DataDir returns the directory the program may write to: portraits extracted
// from the game and downloaded from Valve's CDN live here.
func DataDir() string {
	if override := os.Getenv(dataDirEnv); override != "" {
		return override
	}
	return filepath.Join(xdg.DataHome, appName)
}

// PortraitDir is where reference hero art is stored, named by hero id.
// Alternate portraits (arcanas, personas) are named "{id}_{variant}.png" so
// every file resolves to the same hero id the rest of the project speaks.
func PortraitDir() string { return filepath.Join(DataDir(), "portraits") }

// CaptureDir is where development captures are written by the collect tool.
func CaptureDir() string { return filepath.Join(DataDir(), "captures") }
