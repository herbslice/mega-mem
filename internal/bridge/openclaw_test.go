package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestBridgeOpenClaw_MemoryOnly verifies the bridge captures only the
// workspace's memory/ subdirectory, leaving persona files (SOUL.md,
// IDENTITY.md, USER.md) and runtime state (state/, scripts/) at their
// original location.
func TestBridgeOpenClaw_MemoryOnly(t *testing.T) {
	withFakeHome(t, func(home string) {
		ws := "workspace-test"
		wsRoot := filepath.Join(home, ".openclaw", ws)
		if err := os.MkdirAll(wsRoot, 0o755); err != nil {
			t.Fatalf("mkdir workspace: %v", err)
		}
		for _, name := range []string{"IDENTITY.md", "SOUL.md", "USER.md", "BOOT.md"} {
			if err := os.WriteFile(filepath.Join(wsRoot, name), []byte("persona\n"), 0o644); err != nil {
				t.Fatalf("seed %s: %v", name, err)
			}
		}
		if err := os.MkdirAll(filepath.Join(wsRoot, "state"), 0o755); err != nil {
			t.Fatalf("mkdir state: %v", err)
		}
		if err := os.WriteFile(filepath.Join(wsRoot, "state", "session.json"), []byte("{}"), 0o644); err != nil {
			t.Fatalf("seed state file: %v", err)
		}
		memSrc := filepath.Join(wsRoot, "memory")
		if err := os.MkdirAll(memSrc, 0o755); err != nil {
			t.Fatalf("mkdir memory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(memSrc, "2026-04-29.md"), []byte("# day log\n"), 0o644); err != nil {
			t.Fatalf("seed journal: %v", err)
		}

		vault := t.TempDir()
		if _, err := Bridge(HarnessOpenClaw, vault, Options{
			DryRun:        false,
			Scope:         ws,
			IncludeMemory: true,
		}); err != nil {
			t.Fatalf("Bridge: %v", err)
		}

		for _, name := range []string{"IDENTITY.md", "SOUL.md", "USER.md", "BOOT.md"} {
			if _, err := os.Stat(filepath.Join(wsRoot, name)); err != nil {
				t.Errorf("%s lost from workspace after bridge: %v", name, err)
			}
		}
		if _, err := os.Stat(filepath.Join(wsRoot, "state", "session.json")); err != nil {
			t.Errorf("state/session.json lost: %v", err)
		}
		if !isSymlink(memSrc) {
			t.Errorf("expected memory/ to be a symlink after bridge")
		}
		data, err := os.ReadFile(filepath.Join(memSrc, "2026-04-29.md"))
		if err != nil {
			t.Fatalf("read journal via symlink: %v", err)
		}
		if string(data) != "# day log\n" {
			t.Errorf("journal content = %q after bridge, want %q", data, "# day log\n")
		}
		vaultTarget := filepath.Join(vault, "agent-memory", "openclaw", ws, "2026-04-29.md")
		if _, err := os.Stat(vaultTarget); err != nil {
			t.Errorf("journal not at vault target %s: %v", vaultTarget, err)
		}
	})
}

// TestBridgeOpenClaw_MultiWorkspaceFanOut verifies that --memory with no
// scope bridges every workspace under ~/.openclaw/.
func TestBridgeOpenClaw_MultiWorkspaceFanOut(t *testing.T) {
	withFakeHome(t, func(home string) {
		for _, ws := range []string{"work", "personal", "research"} {
			memDir := filepath.Join(home, ".openclaw", ws, "memory")
			if err := os.MkdirAll(memDir, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", memDir, err)
			}
			if err := os.WriteFile(filepath.Join(memDir, "log.md"), []byte("entry "+ws+"\n"), 0o644); err != nil {
				t.Fatalf("seed %s: %v", ws, err)
			}
		}
		// Index dir that should be ignored as a workspace.
		if err := os.MkdirAll(filepath.Join(home, ".openclaw", "memory"), 0o755); err != nil {
			t.Fatalf("mkdir index: %v", err)
		}

		vault := t.TempDir()
		if _, err := Bridge(HarnessOpenClaw, vault, Options{
			DryRun:        false,
			IncludeMemory: true,
		}); err != nil {
			t.Fatalf("Bridge fan-out: %v", err)
		}

		for _, ws := range []string{"work", "personal", "research"} {
			link := filepath.Join(home, ".openclaw", ws, "memory")
			if !isSymlink(link) {
				t.Errorf("workspace %q: expected memory/ to be a symlink", ws)
				continue
			}
			data, err := os.ReadFile(filepath.Join(vault, "agent-memory", "openclaw", ws, "log.md"))
			if err != nil {
				t.Errorf("workspace %q: vault file missing: %v", ws, err)
			}
			want := "entry " + ws + "\n"
			if string(data) != want {
				t.Errorf("workspace %q: vault content = %q, want %q", ws, data, want)
			}
		}
	})
}

// TestBridgeOpenClaw_MCPOnly verifies the default MCP-only path doesn't
// touch any workspace memory dirs.
func TestBridgeOpenClaw_MCPOnly(t *testing.T) {
	withFakeHome(t, func(home string) {
		ws := "workspace-test"
		memSrc := filepath.Join(home, ".openclaw", ws, "memory")
		if err := os.MkdirAll(memSrc, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		vault := t.TempDir()
		if _, err := Bridge(HarnessOpenClaw, vault, Options{DryRun: false}); err != nil {
			t.Fatalf("Bridge: %v", err)
		}

		if isSymlink(memSrc) {
			t.Errorf("default Bridge replaced memory dir with symlink; expected MCP-only")
		}
		// MCP entry exists.
		configPath := filepath.Join(home, ".openclaw", "openclaw.json")
		raw, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		var cfg map[string]any
		_ = json.Unmarshal(raw, &cfg)
		mcp, _ := cfg["mcp"].(map[string]any)
		servers, _ := mcp["servers"].(map[string]any)
		if _, ok := servers[mcpServerName]; !ok {
			t.Errorf("mega-mem MCP entry missing after bridge: %+v", servers)
		}
	})
}

// TestUnbridgeOpenClaw_MemoryOnly verifies unbridge restores the memory
// subdirectory without touching the rest of the workspace.
func TestUnbridgeOpenClaw_MemoryOnly(t *testing.T) {
	withFakeHome(t, func(home string) {
		ws := "workspace-test"
		wsRoot := filepath.Join(home, ".openclaw", ws)
		if err := os.MkdirAll(wsRoot, 0o755); err != nil {
			t.Fatalf("mkdir workspace: %v", err)
		}
		if err := os.WriteFile(filepath.Join(wsRoot, "IDENTITY.md"), []byte("identity\n"), 0o644); err != nil {
			t.Fatalf("seed IDENTITY: %v", err)
		}

		vault := t.TempDir()
		if _, err := Bridge(HarnessOpenClaw, vault, Options{
			DryRun:        false,
			Scope:         ws,
			IncludeMemory: true,
		}); err != nil {
			t.Fatalf("setup bridge: %v", err)
		}
		journalPath := filepath.Join(wsRoot, "memory", "2026-04-29.md")
		if err := os.WriteFile(journalPath, []byte("after-bridge\n"), 0o644); err != nil {
			t.Fatalf("write through symlink: %v", err)
		}

		if _, err := Unbridge(HarnessOpenClaw, vault, Options{
			DryRun:        false,
			Scope:         ws,
			IncludeMemory: true,
		}); err != nil {
			t.Fatalf("Unbridge: %v", err)
		}

		if isSymlink(filepath.Join(wsRoot, "memory")) {
			t.Errorf("memory/ still a symlink after unbridge")
		}
		data, err := os.ReadFile(journalPath)
		if err != nil {
			t.Fatalf("read restored journal: %v", err)
		}
		if string(data) != "after-bridge\n" {
			t.Errorf("restored content = %q, want %q", data, "after-bridge\n")
		}
		if _, err := os.Stat(filepath.Join(wsRoot, "IDENTITY.md")); err != nil {
			t.Errorf("IDENTITY.md lost during unbridge: %v", err)
		}
	})
}

// TestListOpenClawScopes verifies scope enumeration excludes the index dir.
func TestListOpenClawScopes(t *testing.T) {
	withFakeHome(t, func(home string) {
		root := filepath.Join(home, ".openclaw")
		for _, name := range []string{"work", "personal", "memory"} {
			if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
		}
		got, err := listOpenClawScopes()
		if err != nil {
			t.Fatalf("listOpenClawScopes: %v", err)
		}
		sort.Strings(got)
		want := []string{"personal", "work"}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("scopes = %v, want %v", got, want)
		}
	})
}
