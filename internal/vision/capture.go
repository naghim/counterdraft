package vision

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/kbinani/screenshot"
)

// DefaultTimeout bounds a single screen grab. A capture that never returns -
// a GPU driver reset, a secure-desktop UAC prompt, Dota's own fullscreen
// transitions mid-capture - must not wedge the whole program, so callers get a
// timeout instead of a hang.
const DefaultTimeout = 2 * time.Second

// captureDisplay is a seam for tests. It grabs the primary display, index 0;
// index 0 in some libraries is the union of every monitor, which on a
// multi-screen setup is much wider than any coordinate work expects. Here the
// library is explicit: CaptureDisplay(0) is the primary display.
var captureDisplay = func() (image.Image, error) {
	img, err := screenshot.CaptureDisplay(0)
	if err != nil {
		return nil, err
	}
	return img, nil
}

// ScreenSize returns the width and height of the primary monitor, in pixels.
func ScreenSize() (int, int, error) {
	b := screenshot.GetDisplayBounds(0)
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return 0, 0, fmt.Errorf("vision: no active display")
	}
	return b.Dx(), b.Dy(), nil
}

// Grab returns a screenshot of the primary monitor as an image.
func Grab() (image.Image, error) { return captureDisplay() }

// capturePool is one dedicated worker, reused rather than spawned per call. If a
// capture ever wedges, a fresh worker would just queue up behind the same stuck
// one - a pool of one makes that failure mode visible as "every future call
// times out" instead of an unbounded backlog of goroutines blocked on the same
// dead screen grab.
type capturePool struct {
	requests chan chan image.Image
}

func newCapturePool() *capturePool {
	p := &capturePool{requests: make(chan chan image.Image)}
	go func() {
		for reply := range p.requests {
			img, err := captureDisplay()
			if err != nil {
				img = nil
			}
			reply <- img
		}
	}()
	return p
}

var sharedCapture = newCapturePool()

// GrabWithTimeout is like Grab, but gives up after timeout instead of hanging
// forever. It returns nil on a timeout.
//
// A stuck capture leaks its worker goroutine (Go cannot kill a goroutine
// blocked in a native call), but the pool is sized for exactly that: once the
// one worker is wedged, every later call times out immediately rather than
// queuing behind it.
func GrabWithTimeout(timeout time.Duration) image.Image {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	reply := make(chan image.Image, 1)
	select {
	case sharedCapture.requests <- reply:
	case <-time.After(timeout):
		return nil
	}
	select {
	case img := <-reply:
		return img
	case <-time.After(timeout):
		return nil
	}
}

// Save writes a capture under a timestamped name and returns the path.
func Save(image image.Image, prefix string, directory string) (string, error) {
	if directory == "" {
		directory = CaptureDir()
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", err
	}
	stamp := time.Now().Format("20060102-150405")
	path := filepath.Join(directory, fmt.Sprintf("%s-%s.png", prefix, stamp))
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := png.Encode(f, image); err != nil {
		return "", err
	}
	return path, nil
}
