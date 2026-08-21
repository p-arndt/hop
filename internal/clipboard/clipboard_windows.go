package clipboard

// Done in-process because clip.exe would mangle non-ASCII through the console code page.
// The allocation must be freed on every path that does not reach a successful SetClipboardData.

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

// Only one process may hold the clipboard, so a brief refusal is retried.
const (
	openAttempts = 5
	openDelay    = 20 * time.Millisecond
)

func write(text string) error {
	// The clipboard belongs to whichever thread opened it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// UTF16FromString refuses a NUL; the caller has already dropped control characters.
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

// openClipboard takes the clipboard, retrying a refusal up to openAttempts times.
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

// allocGlobal copies the text into the moveable global allocation SetClipboardData takes.
// RtlMoveMemory avoids turning the locked uintptr back into a pointer, which `go vet` reports.
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
