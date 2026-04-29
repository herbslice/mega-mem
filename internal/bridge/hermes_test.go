package bridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBridgeHermes_FreshSetup(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		res, err := Bridge(HarnessHermes, "shared", vault, Options{DryRun: false})
		if err != nil {
			t.Fatalf("Bridge: %v", err)
		}
		if res.Harness != HarnessHermes || res.Scope != "shared" {
			t.Errorf("result harness/scope mismatch: %+v", res)
		}
		link := filepath.Join(home, ".hermes", "memories")
		target, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("readlink %s: %v", link, err)
		}
		want := filepath.Join(vault, "agent-memory", "hermes", "shared")
		if target != want {
			t.Errorf("symlink target = %q, want %q", target, want)
		}
	})
}

// TestBridgeHermes_AtomicRenameSafety verifies that Hermes's atomic
// rename pattern (temp file + os.Rename within the memories dir) keeps
// working after the bridge: writes go through to the vault subtree, the
// symlink survives.
func TestBridgeHermes_AtomicRenameSafety(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		if _, err := Bridge(HarnessHermes, "shared", vault, Options{DryRun: false}); err != nil {
			t.Fatalf("Bridge: %v", err)
		}

		memDir := filepath.Join(home, ".hermes", "memories")
		// Simulate Hermes's write pattern: write to temp, rename to MEMORY.md.
		tmp := filepath.Join(memDir, "MEMORY.md.tmp")
		if err := os.WriteFile(tmp, []byte("entry one\n§\nentry two\n"), 0o644); err != nil {
			t.Fatalf("write temp: %v", err)
		}
		final := filepath.Join(memDir, "MEMORY.md")
		if err := os.Rename(tmp, final); err != nil {
			t.Fatalf("rename: %v", err)
		}

		// Symlink should still be a symlink.
		if !isSymlink(memDir) {
			t.Errorf("symlink at %s replaced by real entry after rename", memDir)
		}
		// File should be readable through the symlink and through the vault path.
		viaSymlink, err := os.ReadFile(final)
		if err != nil {
			t.Fatalf("read via symlink: %v", err)
		}
		viaVault, err := os.ReadFile(filepath.Join(vault, "agent-memory", "hermes", "shared", "MEMORY.md"))
		if err != nil {
			t.Fatalf("read via vault: %v", err)
		}
		if string(viaSymlink) != string(viaVault) {
			t.Errorf("vault and symlinked views differ: %q vs %q", viaSymlink, viaVault)
		}
	})
}

func TestBridgeHermes_AddsMCPServer(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		// Pre-seed config.yaml with unrelated content so we can verify
		// preservation.
		configPath := filepath.Join(home, ".hermes", "config.yaml")
		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		original := `model:
  default: gpt-5.5
  provider: openai-codex
agent:
  max_turns: 90
`
		if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
			t.Fatalf("seed config: %v", err)
		}

		if _, err := Bridge(HarnessHermes, "shared", vault, Options{DryRun: false}); err != nil {
			t.Fatalf("Bridge: %v", err)
		}

		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		var got map[string]any
		if err := yaml.Unmarshal(data, &got); err != nil {
			t.Fatalf("parse config: %v", err)
		}
		// Pre-existing fields preserved.
		if model, ok := got["model"].(map[string]any); !ok || model["default"] != "gpt-5.5" {
			t.Errorf("model.default lost or mutated: %+v", got["model"])
		}
		// mcp_servers added.
		servers, ok := got["mcp_servers"].(map[string]any)
		if !ok {
			t.Fatalf("mcp_servers not added: %+v", got)
		}
		entry, ok := servers[mcpServerName].(map[string]any)
		if !ok {
			t.Fatalf("mega-mem entry missing: %+v", servers)
		}
		if entry["url"] != DefaultMCPURL {
			t.Errorf("mcp_servers.mega-mem.url = %v, want %s", entry["url"], DefaultMCPURL)
		}
	})
}

func TestBridgeHermes_SkipMCPLeavesConfigAlone(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		configPath := filepath.Join(home, ".hermes", "config.yaml")

		if _, err := Bridge(HarnessHermes, "shared", vault, Options{DryRun: false, SkipMCP: true}); err != nil {
			t.Fatalf("Bridge: %v", err)
		}

		// Config should NOT have been created.
		if _, err := os.Stat(configPath); !os.IsNotExist(err) {
			t.Errorf("expected no config file when SkipMCP, but got %v", err)
		}
		// Memory link should be present.
		if !isSymlink(filepath.Join(home, ".hermes", "memories")) {
			t.Errorf("memory symlink missing")
		}
	})
}

func TestUnbridgeHermes_RoundTrip(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		if _, err := Bridge(HarnessHermes, "shared", vault, Options{DryRun: false}); err != nil {
			t.Fatalf("setup bridge: %v", err)
		}
		// Write content through the symlink.
		notePath := filepath.Join(home, ".hermes", "memories", "MEMORY.md")
		if err := os.WriteFile(notePath, []byte("test content\n"), 0o644); err != nil {
			t.Fatalf("write through symlink: %v", err)
		}

		if _, err := Unbridge(HarnessHermes, "shared", vault, Options{DryRun: false}); err != nil {
			t.Fatalf("Unbridge: %v", err)
		}

		memDir := filepath.Join(home, ".hermes", "memories")
		if isSymlink(memDir) {
			t.Errorf("expected real directory after unbridge, still a symlink")
		}
		data, err := os.ReadFile(filepath.Join(memDir, "MEMORY.md"))
		if err != nil {
			t.Fatalf("read restored MEMORY.md: %v", err)
		}
		if string(data) != "test content\n" {
			t.Errorf("restored content = %q, want %q", string(data), "test content\n")
		}
		// MCP entry should be removed from config.
		configPath := filepath.Join(home, ".hermes", "config.yaml")
		if data, err := os.ReadFile(configPath); err == nil {
			if strings.Contains(string(data), mcpServerName) {
				t.Errorf("mcp_servers.mega-mem still present after unbridge: %s", data)
			}
		}
	})
}
