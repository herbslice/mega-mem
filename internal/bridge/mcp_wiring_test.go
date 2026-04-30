package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBridgeClaudeCode_AddsMCPServer verifies the default (MCP-only)
// bridge wires mcpServers["mega-mem"] in settings.json.
func TestBridgeClaudeCode_AddsMCPServer(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		if _, err := Bridge(HarnessClaudeCode, vault, Options{DryRun: false}); err != nil {
			t.Fatalf("Bridge: %v", err)
		}

		settingsPath := filepath.Join(home, ".claude", "settings.json")
		raw, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("read settings: %v", err)
		}
		var settings map[string]any
		if err := json.Unmarshal(raw, &settings); err != nil {
			t.Fatalf("parse settings: %v", err)
		}
		servers, ok := settings["mcpServers"].(map[string]any)
		if !ok {
			t.Fatalf("mcpServers missing in settings: %+v", settings)
		}
		entry, ok := servers["mega-mem"].(map[string]any)
		if !ok {
			t.Fatalf("mega-mem entry missing: %+v", servers)
		}
		if entry["url"] != DefaultMCPURL {
			t.Errorf("mega-mem.url = %v, want %s", entry["url"], DefaultMCPURL)
		}
	})
}

func TestBridgeClaudeCode_PreservesOtherMCPServers(t *testing.T) {
	withFakeHome(t, func(home string) {
		settingsPath := filepath.Join(home, ".claude", "settings.json")
		if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		original := map[string]any{
			"mcpServers": map[string]any{
				"other-server": map[string]any{
					"url": "http://localhost:9999/sse",
				},
			},
		}
		raw, _ := json.Marshal(original)
		if err := os.WriteFile(settingsPath, raw, 0o644); err != nil {
			t.Fatalf("seed settings: %v", err)
		}

		vault := t.TempDir()
		if _, err := Bridge(HarnessClaudeCode, vault, Options{DryRun: false}); err != nil {
			t.Fatalf("Bridge: %v", err)
		}

		data, _ := os.ReadFile(settingsPath)
		var settings map[string]any
		_ = json.Unmarshal(data, &settings)
		servers, _ := settings["mcpServers"].(map[string]any)
		if _, ok := servers["other-server"]; !ok {
			t.Errorf("other-server lost after bridge: %+v", servers)
		}
		if _, ok := servers["mega-mem"]; !ok {
			t.Errorf("mega-mem not added: %+v", servers)
		}
	})
}

func TestBridgeCodex_AddsMCPSection(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		if _, err := Bridge(HarnessCodex, vault, Options{DryRun: false}); err != nil {
			t.Fatalf("Bridge: %v", err)
		}
		configPath := filepath.Join(home, ".codex", "config.toml")
		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read config.toml: %v", err)
		}
		body := string(data)
		if !strings.Contains(body, "[mcp_servers.mega-mem]") {
			t.Errorf("missing [mcp_servers.mega-mem] section: %s", body)
		}
		if !strings.Contains(body, DefaultMCPURL) {
			t.Errorf("missing url=%s in config: %s", DefaultMCPURL, body)
		}
	})
}

func TestBridgeCodex_PreservesExistingTomlContent(t *testing.T) {
	withFakeHome(t, func(home string) {
		configPath := filepath.Join(home, ".codex", "config.toml")
		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		original := `model = "gpt-5.5"
model_reasoning_effort = "xhigh"
[projects."/home/user"]
trust_level = "trusted"
`
		if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
			t.Fatalf("seed config: %v", err)
		}

		vault := t.TempDir()
		if _, err := Bridge(HarnessCodex, vault, Options{DryRun: false}); err != nil {
			t.Fatalf("Bridge: %v", err)
		}

		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		body := string(data)
		for _, line := range []string{
			`model = "gpt-5.5"`,
			`model_reasoning_effort = "xhigh"`,
			`[projects."/home/user"]`,
			`trust_level = "trusted"`,
		} {
			if !strings.Contains(body, line) {
				t.Errorf("original line lost after MCP add: %q\nbody:\n%s", line, body)
			}
		}
		if !strings.Contains(body, "[mcp_servers.mega-mem]") {
			t.Errorf("mcp_servers section not appended: %s", body)
		}
	})
}

func TestUnbridgeCodex_RemovesOnlyMCPSection(t *testing.T) {
	withFakeHome(t, func(home string) {
		configPath := filepath.Join(home, ".codex", "config.toml")
		if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		original := `model = "gpt-5.5"
[projects."/home/user"]
trust_level = "trusted"

[mcp_servers.mega-mem]
url = "http://127.0.0.1:8111/sse"

[mcp_servers.other]
url = "http://localhost:9999/sse"
`
		if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
			t.Fatalf("seed config: %v", err)
		}

		// MCP-only unbridge — no IncludeMemory, so symlink path is skipped.
		fakeVault := t.TempDir()
		if _, err := Unbridge(HarnessCodex, fakeVault, Options{DryRun: false}); err != nil {
			t.Fatalf("Unbridge: %v", err)
		}

		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		body := string(data)
		if strings.Contains(body, "[mcp_servers.mega-mem]") {
			t.Errorf("[mcp_servers.mega-mem] still present after unbridge: %s", body)
		}
		if !strings.Contains(body, "[mcp_servers.other]") {
			t.Errorf("[mcp_servers.other] removed (should be preserved): %s", body)
		}
		if !strings.Contains(body, `model = "gpt-5.5"`) {
			t.Errorf("model key lost: %s", body)
		}
	})
}

func TestBridgeOpenClaw_AddsMCPServer(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		if _, err := Bridge(HarnessOpenClaw, vault, Options{DryRun: false}); err != nil {
			t.Fatalf("Bridge: %v", err)
		}

		configPath := filepath.Join(home, ".openclaw", "openclaw.json")
		raw, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("read openclaw.json: %v", err)
		}
		var cfg map[string]any
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatalf("parse openclaw.json: %v", err)
		}
		mcp, ok := cfg["mcp"].(map[string]any)
		if !ok {
			t.Fatalf("mcp block missing: %+v", cfg)
		}
		servers, ok := mcp["servers"].(map[string]any)
		if !ok {
			t.Fatalf("mcp.servers missing: %+v", mcp)
		}
		entry, ok := servers[mcpServerName].(map[string]any)
		if !ok {
			t.Fatalf("mega-mem entry missing: %+v", servers)
		}
		if entry["url"] != DefaultMCPURL {
			t.Errorf("mega-mem.url = %v, want %s", entry["url"], DefaultMCPURL)
		}
		if entry["transport"] != "http" {
			t.Errorf("mega-mem.transport = %v, want http", entry["transport"])
		}
	})
}

// TestBridge_SkipMCPOnlyMemory verifies --memory + --no-mcp produces
// memory bridge only.
func TestBridge_SkipMCPOnlyMemory(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		_, err := Bridge(HarnessClaudeCode, vault, Options{
			DryRun:        false,
			IncludeMemory: true,
			SkipMCP:       true,
		})
		if err != nil {
			t.Fatalf("Bridge: %v", err)
		}
		settingsPath := filepath.Join(home, ".claude", "settings.json")
		if data, err := os.ReadFile(settingsPath); err == nil {
			var settings map[string]any
			_ = json.Unmarshal(data, &settings)
			if _, ok := settings["mcpServers"]; ok {
				t.Errorf("mcpServers added despite SkipMCP=true: %+v", settings)
			}
		}
		// Memory symlink should exist.
		projects := filepath.Join(home, ".claude", "projects")
		if !isSymlink(projects) {
			t.Errorf("expected projects symlink with --memory --no-mcp")
		}
	})
}

// TestBridge_DefaultIsMCPOnly verifies the new ergonomic default: bare
// Bridge() only wires MCP, no filesystem moves.
func TestBridge_DefaultIsMCPOnly(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		_, err := Bridge(HarnessClaudeCode, vault, Options{DryRun: false})
		if err != nil {
			t.Fatalf("Bridge: %v", err)
		}
		// MCP wired.
		settingsPath := filepath.Join(home, ".claude", "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("read settings: %v", err)
		}
		var settings map[string]any
		_ = json.Unmarshal(data, &settings)
		if _, ok := settings["mcpServers"]; !ok {
			t.Errorf("mcpServers missing in default mode: %+v", settings)
		}
		// No filesystem moves.
		if _, err := os.Lstat(filepath.Join(home, ".claude", "projects")); !os.IsNotExist(err) {
			t.Errorf("projects symlink/dir created in default mode")
		}
	})
}
