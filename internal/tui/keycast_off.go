//go:build !hopdemo

package tui

// No-op stand-ins for the `hopdemo`-only keycast overlay (see keycast.go); the model
// still carries the field, so View and Update need no build tags of their own.

type keycastState struct{}

func (m *model) keycastRecord(string)             {}
func (m *model) keycastDraw(screen string) string { return screen }
