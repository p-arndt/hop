package clipboard

// The Windows clipboard is an OS service rather than a program, so this is the one
// platform doing the work in-process. The obvious helper, clip.exe, reads its input in
// the console's code page, so anything outside ASCII arrives as mojibake — and text
// copied off a remote host is exactly where a UTF-8 character turns up.
//
// The sequence is the documented one: open the clipboard, empty it, hand it a moveable
// global allocation holding UTF-16, close it. Two details matter: ownership of the
// allocation passes to the system on a successful SetClipboardData, so it must be freed
// on every path that does not reach one; and the clipboard is owned by a thread, which is
// why the goroutine is pinned to one.

import (
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procSetClipboardData = user32.NewProc("SetClipboardData")

	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalAlloc   = kernel32.NewProc("GlobalAlloc")
	procGlobalFree    = kernel32.NewProc("GlobalFree")
	procGlobalLock    = kernel32.NewProc("GlobalLock")
	procGlobalUnlock  = kernel32.NewProc("GlobalUnlock")
	procRtlMoveMemory = kernel32.NewProc("RtlMoveMemory")
)

const (
	cfUnicodeText = 13     // CF_UNICODETEXT
	gmemMoveable  = 0x0002 // GMEM_MOVEABLE, which is what the clipboard requires
)

// openAttempts and openDelay are how hard hop tries to take the clipboard. Only one
// process may hold it at a time, and something else holding it for a moment is ordinary,
// so a refusal is retried briefly before it is reported.
const (
	openAttempts = 5
	openDelay    = 20 * time.Millisecond
)

func write(text string) error {
	// The clipboard belongs to whichever thread opened it, and a goroutine can move
	// between threads at any suspension point.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// UTF16FromString refuses a string containing a NUL. The caller has already dropped
	// the control characters, so this is a guard rather than a live case.
	utf16, err := windows.UTF16FromString(text)
	if err != nil {
		return fmt.Errorf("clipboard: encode: %w", err)
	}

	if err := openClipboard(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()

	mem, err := allocGlobal(utf16)
	if err != nil {
		return err
	}

	// Emptying is what makes hop the clipboard's owner; without it SetClipboardData fails.
	if ret, _, err := procEmptyClipboard.Call(); ret == 0 {
		procGlobalFree.Call(mem)
		return fmt.Errorf("clipboard: empty: %w", err)
	}

	if ret, _, err := procSetClipboardData.Call(cfUnicodeText, mem); ret == 0 {
		// Still ours, because the system did not take it.
		procGlobalFree.Call(mem)
		return fmt.Errorf("clipboard: set: %w", err)
	}
	// From here the allocation belongs to the system.
	return nil
}

// openClipboard takes the clipboard, retrying a refusal as far as openAttempts allows.
func openClipboard() error {
	var lastErr error
	for i := 0; i < openAttempts; i++ {
		ret, _, err := procOpenClipboard.Call(0)
		if ret != 0 {
			return nil
		}
		lastErr = err
		time.Sleep(openDelay)
	}
	return fmt.Errorf("clipboard: open: %w", lastErr)
}

// allocGlobal copies the encoded text into a moveable global allocation, the form
// SetClipboardData takes. The returned handle is the caller's to free until the clipboard
// has accepted it.
//
// The copy goes through RtlMoveMemory rather than a Go slice over the locked address:
// building that slice means converting a returned uintptr back into a pointer, which the
// garbage collector makes no promises about and `go vet` reports.
func allocGlobal(utf16 []uint16) (uintptr, error) {
	size := uintptr(len(utf16)) * unsafe.Sizeof(utf16[0])

	mem, _, err := procGlobalAlloc.Call(gmemMoveable, size)
	if mem == 0 {
		return 0, fmt.Errorf("clipboard: alloc: %w", err)
	}

	ptr, _, err := procGlobalLock.Call(mem)
	if ptr == 0 {
		procGlobalFree.Call(mem)
		return 0, fmt.Errorf("clipboard: lock: %w", err)
	}
	procRtlMoveMemory.Call(ptr, uintptr(unsafe.Pointer(&utf16[0])), size)
	procGlobalUnlock.Call(mem)

	return mem, nil
}
