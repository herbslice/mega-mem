package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withFakeHome runs fn with $HOME pointed at a fresh tempdir, restoring the
// original after. Used to isolate tests from the developer's real ~/.claude
// or ~/.codex directories.
func withFakeHome(t *testing.T, fn func(home string)) {
	t.Helper()
	tmp := t.TempDir()
	orig, hadOrig := os.LookupEnv("HOME")
	t.Setenv("HOME", tmp)
	t.Cleanup(func() {
		if hadOrig {
			os.Setenv("HOME", orig)
		} else {
			os.Unsetenv("HOME")
		}
	})
	fn(tmp)
}

func TestParseHarness(t *testing.T) {
	cases := map[string]struct {
		want    Harness
		wantErr bool
	}{
		"claude-code": {want: HarnessClaudeCode},
		"codex":       {want: HarnessCodex},
		"openclaw":    {want: HarnessOpenClaw},
		"hermes":      {want: HarnessHermes},
		"unknown":     {wantErr: true},
		"":            {wantErr: true},
		"Codex":       {wantErr: true},
	}
	for input, c := range cases {
		got, err := ParseHarness(input)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseHarness(%q) = %q, want error", input, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseHarness(%q) returned error: %v", input, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseHarness(%q) = %q, want %q", input, got, c.want)
		}
	}
}

// TestBridgeCodex_MemoryOptIn verifies the new MCP-only default: a bare
// Bridge call only wires MCP, leaving ~/.codex/memories/ untouched.
func TestBridgeCodex_MemoryOptIn(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		// Default options = MCP only.
		if _, err := Bridge(HarnessCodex, vault, Options{DryRun: false}); err != nil {
			t.Fatalf("Bridge: %v", err)
		}
		// No symlink should have been created.
		link := filepath.Join(home, ".codex", "memories")
		if _, err := os.Lstat(link); !os.IsNotExist(err) {
			t.Errorf("default Bridge created %s; expected MCP-only", link)
		}
		// MCP config should exist.
		if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); err != nil {
			t.Errorf("MCP config not written: %v", err)
		}
	})
}

func TestBridgeCodex_FreshSetupWithMemory(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		res, err := Bridge(HarnessCodex, vault, Options{
			DryRun:        false,
			Scope:         "personal",
			IncludeMemory: true,
		})
		if err != nil {
			t.Fatalf("Bridge: %v", err)
		}
		if res.Harness != HarnessCodex || res.Scope != "personal" {
			t.Errorf("result harness/scope mismatch: %+v", res)
		}
		link := filepath.Join(home, ".codex", "memories")
		target, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("readlink %s: %v", link, err)
		}
		want := filepath.Join(vault, "agent-memory", "codex", "personal")
		if target != want {
			t.Errorf("symlink target = %q, want %q", target, want)
		}
		if !dirExists(want) {
			t.Errorf("vault subdir %s does not exist after bridge", want)
		}
	})
}

func TestBridgeCodex_DefaultScopeUsesMemoriesPath(t *testing.T) {
	// Empty scope on a single-dir harness defaults to vault subdir "memories".
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		if _, err := Bridge(HarnessCodex, vault, Options{
			DryRun:        false,
			IncludeMemory: true,
		}); err != nil {
			t.Fatalf("Bridge: %v", err)
		}
		link := filepath.Join(home, ".codex", "memories")
		target, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("readlink %s: %v", link, err)
		}
		want := filepath.Join(vault, "agent-memory", "codex", "memories")
		if target != want {
			t.Errorf("default-scope symlink target = %q, want %q", target, want)
		}
	})
}

func TestBridgeCodex_MigratesExistingFiles(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		src := filepath.Join(home, ".codex", "memories")
		if err := os.MkdirAll(src, 0o755); err != nil {
			t.Fatalf("mkdir src: %v", err)
		}
		if err := os.WriteFile(filepath.Join(src, "note.md"), []byte("# test\n"), 0o644); err != nil {
			t.Fatalf("write note: %v", err)
		}

		if _, err := Bridge(HarnessCodex, vault, Options{
			DryRun:        false,
			Scope:         "personal",
			IncludeMemory: true,
		}); err != nil {
			t.Fatalf("Bridge: %v", err)
		}

		migrated := filepath.Join(vault, "agent-memory", "codex", "personal", "note.md")
		data, err := os.ReadFile(migrated)
		if err != nil {
			t.Fatalf("read migrated file: %v", err)
		}
		if string(data) != "# test\n" {
			t.Errorf("migrated content = %q, want %q", string(data), "# test\n")
		}
		if !isSymlink(src) {
			t.Errorf("expected %s to be a symlink after bridge", src)
		}
	})
}

func TestBridgeCodex_DryRunNoChanges(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		res, err := Bridge(HarnessCodex, vault, Options{
			DryRun:        true,
			Scope:         "personal",
			IncludeMemory: true,
		})
		if err != nil {
			t.Fatalf("Bridge dry-run: %v", err)
		}
		if !res.DryRun {
			t.Errorf("expected DryRun=true on result")
		}
		if res.Executed != 0 {
			t.Errorf("dry-run executed %d steps, want 0", res.Executed)
		}
		link := filepath.Join(home, ".codex", "memories")
		if _, err := os.Lstat(link); !os.IsNotExist(err) {
			t.Errorf("dry-run created %s; expected no changes", link)
		}
	})
}

// TestBridgeClaudeCode_MemoryDefaultMCPOnly confirms that without
// IncludeMemory, no symlink touches ~/.claude/projects/.
func TestBridgeClaudeCode_MemoryDefaultMCPOnly(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		if _, err := Bridge(HarnessClaudeCode, vault, Options{DryRun: false}); err != nil {
			t.Fatalf("Bridge: %v", err)
		}
		projects := filepath.Join(home, ".claude", "projects")
		if _, err := os.Lstat(projects); !os.IsNotExist(err) {
			t.Errorf("default Bridge created %s; expected MCP-only", projects)
		}
		// MCP entry was added.
		settings, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
		if err != nil {
			t.Fatalf("settings.json missing: %v", err)
		}
		if !strings.Contains(string(settings), `"mega-mem"`) {
			t.Errorf("mega-mem not in mcpServers: %s", settings)
		}
	})
}

// TestBridgeClaudeCode_MemorySymlinksProjects verifies that --memory with
// no scope symlinks the whole ~/.claude/projects/ dir.
func TestBridgeClaudeCode_MemorySymlinksProjects(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		if _, err := Bridge(HarnessClaudeCode, vault, Options{
			DryRun:        false,
			IncludeMemory: true,
		}); err != nil {
			t.Fatalf("Bridge: %v", err)
		}
		projects := filepath.Join(home, ".claude", "projects")
		if !isSymlink(projects) {
			t.Errorf("expected %s to be a symlink", projects)
		}
		target, err := os.Readlink(projects)
		if err != nil {
			t.Fatalf("readlink: %v", err)
		}
		want := filepath.Join(vault, "agent-memory", "claude-code", "projects")
		if target != want {
			t.Errorf("symlink target = %q, want %q", target, want)
		}
	})
}

// TestBridgeClaudeCode_PreservesOtherSettings verifies that bridging
// preserves unrelated settings.json content (like statusLine).
func TestBridgeClaudeCode_PreservesOtherSettings(t *testing.T) {
	withFakeHome(t, func(home string) {
		settingsPath := filepath.Join(home, ".claude", "settings.json")
		if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		original := map[string]any{
			"statusLine": map[string]any{
				"type":    "command",
				"command": "/path/to/statusline.sh",
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

		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("read settings: %v", err)
		}
		var settings map[string]any
		if err := json.Unmarshal(data, &settings); err != nil {
			t.Fatalf("parse settings: %v", err)
		}
		statusLine, ok := settings["statusLine"].(map[string]any)
		if !ok {
			t.Fatalf("statusLine missing after bridge: %+v", settings)
		}
		if statusLine["command"] != "/path/to/statusline.sh" {
			t.Errorf("statusLine clobbered: %+v", statusLine)
		}
		if _, ok := settings["mcpServers"]; !ok {
			t.Errorf("mcpServers not added")
		}
	})
}

func TestUnbridgeCodex_RestoresFiles(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		if _, err := Bridge(HarnessCodex, vault, Options{
			DryRun:        false,
			Scope:         "personal",
			IncludeMemory: true,
		}); err != nil {
			t.Fatalf("setup bridge: %v", err)
		}
		notePath := filepath.Join(home, ".codex", "memories", "after-bridge.md")
		if err := os.WriteFile(notePath, []byte("# post\n"), 0o644); err != nil {
			t.Fatalf("write through symlink: %v", err)
		}

		if _, err := Unbridge(HarnessCodex, vault, Options{
			DryRun:        false,
			Scope:         "personal",
			IncludeMemory: true,
		}); err != nil {
			t.Fatalf("Unbridge: %v", err)
		}

		link := filepath.Join(home, ".codex", "memories")
		if isSymlink(link) {
			t.Errorf("expected %s to be a real directory after unbridge, still a symlink", link)
		}
		data, err := os.ReadFile(filepath.Join(link, "after-bridge.md"))
		if err != nil {
			t.Fatalf("read restored file: %v", err)
		}
		if string(data) != "# post\n" {
			t.Errorf("restored content = %q, want %q", string(data), "# post\n")
		}
	})
}

func TestBridgeRefusesConflictingExistingSymlink(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		other := t.TempDir()
		link := filepath.Join(home, ".codex", "memories")
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.Symlink(other, link); err != nil {
			t.Fatalf("symlink: %v", err)
		}

		_, err := Bridge(HarnessCodex, vault, Options{
			DryRun:        false,
			Scope:         "personal",
			IncludeMemory: true,
		})
		if err == nil {
			t.Errorf("expected error when symlink exists pointing elsewhere")
		}
		if err != nil && !strings.Contains(err.Error(), "already symlinked") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// TestListClaudeScopes verifies project-slug enumeration works.
func TestListClaudeScopes(t *testing.T) {
	withFakeHome(t, func(home string) {
		projects := filepath.Join(home, ".claude", "projects")
		for _, slug := range []string{"-tmp-foo", "-tmp-bar", "-tmp-baz"} {
			if err := os.MkdirAll(filepath.Join(projects, slug), 0o755); err != nil {
				t.Fatalf("mkdir slug: %v", err)
			}
		}
		got, err := listClaudeScopes()
		if err != nil {
			t.Fatalf("listClaudeScopes: %v", err)
		}
		if len(got) != 3 {
			t.Errorf("got %d scopes, want 3: %v", len(got), got)
		}
	})
}

// TestListScopesViaBridge verifies the public ListScopes path returns
// scopes without applying any steps.
func TestListScopesViaBridge(t *testing.T) {
	withFakeHome(t, func(home string) {
		projects := filepath.Join(home, ".claude", "projects")
		if err := os.MkdirAll(filepath.Join(projects, "-tmp-foo"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		vault := t.TempDir()
		res, err := Bridge(HarnessClaudeCode, vault, Options{ListScopes: true})
		if err != nil {
			t.Fatalf("Bridge --list-scopes: %v", err)
		}
		if len(res.Steps) != 0 {
			t.Errorf("expected no steps in list-scopes mode, got %d", len(res.Steps))
		}
		if len(res.Scopes) == 0 {
			t.Errorf("expected at least one scope, got none")
		}
	})
}
