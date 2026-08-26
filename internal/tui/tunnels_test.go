package tui

import (
	"strings"
	"testing"

	"hop/internal/sshx"
	"hop/internal/store"
)

func TestTunnelManagerAddsEditsAndDeletesDefinition(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "web.example.com"})

	m.handleKey(key(t, "T"))
	if !m.tunnels.open || m.tunnels.alias != "web" {
		t.Fatal("T did not open the selected host's tunnel manager")
	}
	m.handleKey(key(t, "a"))
	m.tunnels.buf[tfBindPort] = "15432"
	m.tunnels.buf[tfTargetHost] = "db.internal"
	m.tunnels.buf[tfTargetPort] = "5432"
	m.handleKey(key(t, "enter"))

	h, ok := m.hostByAlias("web")
	if !ok || len(h.Forwards) != 1 {
		t.Fatalf("saved forwards = %+v, want one", h.Forwards)
	}
	f := h.Forwards[0]
	if f.Kind != store.ForwardLocal || f.BindHost != "127.0.0.1" || f.BindPort != 15432 || f.TargetHost != "db.internal" {
		t.Fatalf("saved forward = %+v", f)
	}

	m.handleKey(key(t, "e"))
	m.tunnels.buf[tfTargetPort] = "6432"
	m.handleKey(key(t, "enter"))
	h, _ = m.hostByAlias("web")
	if len(h.Forwards) != 1 || h.Forwards[0].TargetPort != 6432 {
		t.Fatalf("edited forwards = %+v", h.Forwards)
	}

	m.handleKey(key(t, "x"))
	h, _ = m.hostByAlias("web")
	if len(h.Forwards) != 0 {
		t.Fatalf("deleted definition still present: %+v", h.Forwards)
	}
}

func TestTunnelFormValidationKeepsTypedDefinition(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "web.example.com"})
	m.openTunnels(m.hosts[0])
	m.handleKey(key(t, "a"))
	m.tunnels.buf[tfBindPort] = "70000"
	m.tunnels.buf[tfTargetHost] = "db.internal"
	m.tunnels.buf[tfTargetPort] = "5432"
	m.handleKey(key(t, "enter"))

	if !m.tunnels.editing {
		t.Fatal("invalid definition closed the edit form")
	}
	if m.statusKind != statusErr || !strings.Contains(m.status, "bind port") {
		t.Fatalf("status = %q, want a bind-port error", m.status)
	}
	if m.tunnels.buf[tfTargetHost] != "db.internal" {
		t.Fatal("validation discarded the user's typed fields")
	}
}

func TestPasteGoesOnlyToEditableTunnelFields(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "web.example.com"})
	m.openTunnels(m.hosts[0])
	m.handleKey(key(t, "a"))
	m.tunnels.field = tfTargetHost
	m.handlePaste("db.internal")
	if m.tunnels.buf[tfTargetHost] != "db.internal" {
		t.Fatalf("pasted target = %q", m.tunnels.buf[tfTargetHost])
	}
	m.tunnels.field = tfKind
	m.handlePaste("remote")
	if m.tunnels.buf[tfKind] != string(store.ForwardLocal) {
		t.Fatal("paste overwrote the direction picker")
	}
}

func TestTunnelKeyWithNoDefinitionsOpensManager(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "web.example.com"})
	_, cmd := m.handleKey(key(t, "t"))
	if cmd != nil {
		t.Fatal("t with no definitions tried to connect")
	}
	if !m.tunnels.open {
		t.Fatal("t with no definitions did not open the manager")
	}
}

func TestTunnelDashboardShowsDefinitionsAndRuntimeState(t *testing.T) {
	m := viewModel(120, 34)
	f := store.Forward{ID: 42, Kind: store.ForwardRemote, BindHost: "127.0.0.1", BindPort: 18080, TargetHost: "localhost", TargetPort: 3000}
	m.hosts[0].Forwards = []store.Forward{f}
	m.sessions["web1"] = &session{tunnels: map[int64]*sshx.Tunnel{f.ID: {}}}
	m.cursor = 0

	details := m.renderDetails(m.paneW)
	for _, want := range []string{"TUNNELS", "R", "127.0.0.1:18080", "localhost:3000", "1 tunnel"} {
		if !strings.Contains(details, want) {
			t.Fatalf("details card does not contain %q:\n%s", want, details)
		}
	}
	if row := m.renderRow(m.hosts[0], nil, true, 50); !strings.Contains(row, "⇄1") {
		t.Fatalf("host row has no running-tunnel badge: %q", row)
	}
}

func TestReconnectPlanKeepsRunningTunnelIDs(t *testing.T) {
	s := &session{tunnels: map[int64]*sshx.Tunnel{9: {}, 3: {}}}
	plan := s.plan(false)
	if len(plan.tunnels) != 2 || plan.tunnels[0] != 3 || plan.tunnels[1] != 9 {
		t.Fatalf("plan tunnel ids = %v, want [3 9]", plan.tunnels)
	}
	if got := plan.restored(0); !strings.Contains(got, "2 tunnels") {
		t.Fatalf("restored summary = %q", got)
	}
}

func TestUnknownHostKeyPreservesTunnelIntent(t *testing.T) {
	m := hostMgmtModel(t, store.Host{Alias: "web", HostName: "web.example.com"})
	m.connecting = map[string]bool{"web": true}
	m.tunnelsLanded(tunnelsStartedMsg{
		alias: "web",
		ids:   []int64{4, 8},
		err:   unknownKeyErr("web.example.com:22", "SHA256:tunnel", "ssh-ed25519"),
	})
	if !m.hostKey.open || m.hostKey.action != hostKeyTunnels {
		t.Fatal("unknown key did not open a tunnel-specific confirmation")
	}
	if len(m.hostKey.tunnelIDs) != 2 || m.hostKey.tunnelIDs[1] != 8 {
		t.Fatalf("preserved tunnel ids = %v", m.hostKey.tunnelIDs)
	}
}
