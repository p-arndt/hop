package terminal

// Application cursor keys.
//
// DECCKM is DECSET 1: a full-screen program asks with ESC[?1h, and from then on expects its
// cursor keys as SS3 (ESC O A) rather than CSI (ESC [ A). vim, less and mc all ask. A
// modified cursor key is unaffected — xterm sends CSI 1;<mod>A either way.

import (
	"sync"

	"github.com/charmbracelet/x/ansi"
)

// cursorKeysState is written by the output pump and read by the UI goroutine, hence the mutex.
type cursorKeysState struct {
	mu sync.Mutex
	on bool
}

func (s *cursorKeysState) setMode(mode ansi.Mode, on bool) {
	dec, ok := mode.(ansi.DECMode)
	if !ok || dec != ansi.ModeCursorKeys {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.on = on
}

// clear forgets the mode; called from the output pump on the RIS the mode callbacks do not
// report (see oscScanner.ris).
func (s *cursorKeysState) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.on = false
}

func (s *cursorKeysState) enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.on
}

// AppCursorKeys reports whether the remote program asked for SS3 cursor keys.
func (p *Pane) AppCursorKeys() bool { return p.cursorKeys.enabled() }
