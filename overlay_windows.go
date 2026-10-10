//go:build windows

package main

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

const (
	gwlExStyle      = -20
	wsExTransparent = 0x00000020
	wsExLayered     = 0x00080000
	swpNoSize       = 0x0001
	swpNoMove       = 0x0002
	swpNoZOrder     = 0x0004
	swpFrameChanged = 0x0020
)

var (
	user32                = syscall.NewLazyDLL("user32.dll")
	procFindWindowW       = user32.NewProc("FindWindowW")
	procGetWindowLongPtrW = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
	procSetWindowPos      = user32.NewProc("SetWindowPos")
)

// setClickThrough toggles WS_EX_TRANSPARENT on the overlay window so it ignores
// all mouse input and clicks pass through to Dota 2.
func setClickThrough(enable bool) error {
	hwnd, err := findOverlayWindow()
	if err != nil {
		return err
	}
	ex, _, _ := procGetWindowLongPtrW.Call(uintptr(hwnd), uintptr(gwlExStyle))
	if enable {
		ex |= wsExTransparent | wsExLayered
	} else {
		ex &^= wsExTransparent
	}
	// SetWindowLongPtr returns the previous value, so a zero return is legal
	// and not by itself an error.
	procSetWindowLongPtrW.Call(uintptr(hwnd), uintptr(gwlExStyle), ex)
	procSetWindowPos.Call(uintptr(hwnd), 0, 0, 0, 0, 0,
		swpNoMove|swpNoSize|swpNoZOrder|swpFrameChanged)
	return nil
}

// findOverlayWindow retries briefly because the window may not exist yet when
// the frontend first requests a click-through change.
func findOverlayWindow() (syscall.Handle, error) {
	class, err := syscall.UTF16PtrFromString(overlayWindowClass)
	if err != nil {
		return 0, err
	}
	for i := 0; i < 20; i++ {
		hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(class)), 0)
		if hwnd != 0 {
			return syscall.Handle(hwnd), nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return 0, fmt.Errorf("overlay: window %q not found", overlayWindowClass)
}
