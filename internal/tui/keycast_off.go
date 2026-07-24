//go:build !hopdemo

package tui

// The keycast overlay is a recording aid, built only under the `hopdemo` tag (see
// keycast.go). In every other build it is these no-ops: the model still carries the
// field, so View and Update need no build tags of their own, but it holds nothing
// and draws nothing.

type keycastState struct{}

func (m *model) keycastRecord(string)             {}
func (m *model) keycastDraw(screen string) string { return screen }
