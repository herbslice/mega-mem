package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestBridgeOpenClaw_MemoryOnly verifies the bridge captures only the
// workspace's memory/ subdirectory, leaving persona files (SOUL.md,
// IDENTITY.md, USER.md) and runtime state (state/, scripts/) at their
// original location. This is the Option B scope decision.
func TestBridgeOpenClaw_MemoryOnly(t *testing.T) {
	withFakeHome(t, func(home string) {
		ws := "workspace-test"
		wsRoot := filepath.Join(home, ".openclaw", ws)
		if err := os.MkdirAll(wsRoot, 0o755); err != nil {
			t.Fatalf("mkdir workspace: %v", err)
		}
		// Persona files at workspace root (must stay).
		for _, name := range []string{"IDENTITY.md", "SOUL.md", "USER.md", "BOOT.md"} {
			if err := os.WriteFile(filepath.Join(wsRoot, name), []byte("persona\n"), 0o644); err != nil {
				t.Fatalf("seed %s: %v", name, err)
			}
		}
		// Runtime state subdir (must stay).
		if err := os.MkdirAll(filepath.Join(wsRoot, "state"), 0o755); err != nil {
			t.Fatalf("mkdir state: %v", err)
		}
		if err := os.WriteFile(filepath.Join(wsRoot, "state", "session.json"), []byte("{}"), 0o644); err != nil {
			t.Fatalf("seed state file: %v", err)
		}
		// Memory subdir with a journal entry (this should be bridged).
		memSrc := filepath.Join(wsRoot, "memory")
		if err := os.MkdirAll(memSrc, 0o755); err != nil {
			t.Fatalf("mkdir memory: %v", err)
		}
		if err := os.WriteFile(filepath.Join(memSrc, "2026-04-29.md"), []byte("# day log\n"), 0o644); err != nil {
			t.Fatalf("seed journal: %v", err)
		}

		vault := t.TempDir()
		if _, err := Bridge(HarnessOpenClaw, ws, vault, Options{DryRun: false}); err != nil {
			t.Fatalf("Bridge: %v", err)
		}

		// Persona files still at workspace root.
		for _, name := range []string{"IDENTITY.md", "SOUL.md", "USER.md", "BOOT.md"} {
			if _, err := os.Stat(filepath.Join(wsRoot, name)); err != nil {
				t.Errorf("%s lost from workspace after bridge: %v", name, err)
			}
		}
		// Runtime state still at workspace root.
		if _, err := os.Stat(filepath.Join(wsRoot, "state", "session.json")); err != nil {
			t.Errorf("state/session.json lost: %v", err)
		}
		// Memory subdir is now a symlink.
		if !isSymlink(memSrc) {
			t.Errorf("expected memory/ to be a symlink after bridge")
		}
		// Journal file readable through the symlink.
		data, err := os.ReadFile(filepath.Join(memSrc, "2026-04-29.md"))
		if err != nil {
			t.Fatalf("read journal via symlink: %v", err)
		}
		if string(data) != "# day log\n" {
			t.Errorf("journal content = %q after bridge, want %q", data, "# day log\n")
		}
		// Vault target contains the journal directly (no extra memory/ segment).
		vaultTarget := filepath.Join(vault, "agent-memory", "openclaw", ws, "2026-04-29.md")
		if _, err := os.Stat(vaultTarget); err != nil {
			t.Errorf("journal not at vault target %s: %v", vaultTarget, err)
		}
	})
}

// TestBridgeOpenClaw_DoesNotEditWorkspaceConfig verifies the bridge no
// longer touches agents.defaults.workspace (Option B removes that edit).
// MCP wiring still happens.
func TestBridgeOpenClaw_DoesNotEditWorkspaceConfig(t *testing.T) {
	withFakeHome(t, func(home string) {
		configPath := filepath.Join(home, ".openclaw", "openclaw.json")
		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		// Pre-seed config with an existing agents.defaults.workspace value
		// that should stay untouched.
		original := map[string]any{
			"agents": map[string]any{
				"defaults": map[string]any{
					"workspace": "/home/user/.openclaw/workspace",
				},
			},
		}
		raw, _ := json.Marshal(original)
		if err := os.WriteFile(configPath, raw, 0o644); err != nil {
			t.Fatalf("seed config: %v", err)
		}

		vault := t.TempDir()
		if _, err := Bridge(HarnessOpenClaw, "workspace-test", vault, Options{DryRun: false}); err != nil {
			t.Fatalf("Bridge: %v", err)
		}

		data, _ := os.ReadFile(configPath)
		var got map[string]any
		_ = json.Unmarshal(data, &got)
		agents, _ := got["agents"].(map[string]any)
		defaults, _ := agents["defaults"].(map[string]any)
		ws, _ := defaults["workspace"].(string)
		if ws != "/home/user/.openclaw/workspace" {
			t.Errorf("agents.defaults.workspace = %q, want unchanged %q", ws, "/home/user/.openclaw/workspace")
		}
		// MCP wiring DID happen.
		mcp, ok := got["mcp"].(map[string]any)
		if !ok {
			t.Errorf("mcp block missing after bridge: %+v", got)
		}
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
		// Persona file at workspace root.
		if err := os.WriteFile(filepath.Join(wsRoot, "IDENTITY.md"), []byte("identity\n"), 0o644); err != nil {
			t.Fatalf("seed IDENTITY: %v", err)
		}

		vault := t.TempDir()
		if _, err := Bridge(HarnessOpenClaw, ws, vault, Options{DryRun: false}); err != nil {
			t.Fatalf("setup bridge: %v", err)
		}
		// Write through the symlink.
		journalPath := filepath.Join(wsRoot, "memory", "2026-04-29.md")
		if err := os.WriteFile(journalPath, []byte("after-bridge\n"), 0o644); err != nil {
			t.Fatalf("write through symlink: %v", err)
		}

		if _, err := Unbridge(HarnessOpenClaw, ws, vault, Options{DryRun: false}); err != nil {
			t.Fatalf("Unbridge: %v", err)
		}

		// Memory subdir is real again.
		if isSymlink(filepath.Join(wsRoot, "memory")) {
			t.Errorf("memory/ still a symlink after unbridge")
		}
		// Journal restored.
		data, err := os.ReadFile(journalPath)
		if err != nil {
			t.Fatalf("read restored journal: %v", err)
		}
		if string(data) != "after-bridge\n" {
			t.Errorf("restored content = %q, want %q", data, "after-bridge\n")
		}
		// Persona file still at workspace root.
		if _, err := os.Stat(filepath.Join(wsRoot, "IDENTITY.md")); err != nil {
			t.Errorf("IDENTITY.md lost during unbridge: %v", err)
		}
	})
}
