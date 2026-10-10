//go:build !windows

package main

import "fmt"

// setClickThrough is only implemented on Windows. On Linux/macOS the overlay
// stays interactive; use the header to drag it out of the way.
func setClickThrough(enable bool) error {
	return fmt.Errorf("overlay: click-through is currently only implemented on Windows")
}
