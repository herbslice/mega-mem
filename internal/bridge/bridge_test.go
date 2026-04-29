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
		"unknown":     {wantErr: true},
		"":            {wantErr: true},
		"Codex":       {wantErr: true}, // case-sensitive on purpose; lower-case canonical only
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

func TestBridgeCodex_FreshSetup(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		res, err := Bridge(HarnessCodex, "personal", vault, Options{DryRun: false})
		if err != nil {
			t.Fatalf("Bridge: %v", err)
		}
		if res.Harness != HarnessCodex || res.Scope != "personal" {
			t.Errorf("result harness/scope mismatch: %+v", res)
		}
		// Symlink should exist at ~/.codex/memories pointing into the vault.
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

		if _, err := Bridge(HarnessCodex, "personal", vault, Options{DryRun: false}); err != nil {
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
		// Source should be a symlink now, not the original directory.
		if !isSymlink(src) {
			t.Errorf("expected %s to be a symlink after bridge", src)
		}
	})
}

func TestBridgeCodex_DryRunNoChanges(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		res, err := Bridge(HarnessCodex, "personal", vault, Options{DryRun: true})
		if err != nil {
			t.Fatalf("Bridge dry-run: %v", err)
		}
		if !res.DryRun {
			t.Errorf("expected DryRun=true on result")
		}
		if res.Executed != 0 {
			t.Errorf("dry-run executed %d steps, want 0", res.Executed)
		}
		// No filesystem mutations should have happened.
		link := filepath.Join(home, ".codex", "memories")
		if _, err := os.Lstat(link); !os.IsNotExist(err) {
			t.Errorf("dry-run created %s; expected no changes", link)
		}
	})
}

func TestBridgeClaudeCode_SetsAutoMemoryDirectory(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		if _, err := Bridge(HarnessClaudeCode, "-tmp-fakeproject", vault, Options{DryRun: false}); err != nil {
			t.Fatalf("Bridge: %v", err)
		}
		settingsPath := filepath.Join(home, ".claude", "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatalf("read settings: %v", err)
		}
		var settings map[string]any
		if err := json.Unmarshal(data, &settings); err != nil {
			t.Fatalf("parse settings: %v", err)
		}
		got, ok := settings["autoMemoryDirectory"].(string)
		if !ok {
			t.Fatalf("autoMemoryDirectory not set in %s", settingsPath)
		}
		want := filepath.Join(vault, "agent-memory", "claude-code")
		if got != want {
			t.Errorf("autoMemoryDirectory = %q, want %q", got, want)
		}
	})
}

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
		if _, err := Bridge(HarnessClaudeCode, "-tmp-foo", vault, Options{DryRun: false}); err != nil {
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
		if _, ok := settings["autoMemoryDirectory"]; !ok {
			t.Errorf("autoMemoryDirectory not added")
		}
	})
}

func TestUnbridgeCodex_RestoresFiles(t *testing.T) {
	withFakeHome(t, func(home string) {
		vault := t.TempDir()
		// Bridge first.
		if _, err := Bridge(HarnessCodex, "personal", vault, Options{DryRun: false}); err != nil {
			t.Fatalf("setup bridge: %v", err)
		}
		// Add a file via the (now-symlinked) location, simulating Codex writing memory.
		notePath := filepath.Join(home, ".codex", "memories", "after-bridge.md")
		if err := os.WriteFile(notePath, []byte("# post\n"), 0o644); err != nil {
			t.Fatalf("write through symlink: %v", err)
		}

		// Unbridge.
		if _, err := Unbridge(HarnessCodex, "personal", vault, Options{DryRun: false}); err != nil {
			t.Fatalf("Unbridge: %v", err)
		}

		// ~/.codex/memories should now be a real directory containing the file.
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
		// Pre-create a symlink at ~/.codex/memories pointing somewhere else.
		link := filepath.Join(home, ".codex", "memories")
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.Symlink(other, link); err != nil {
			t.Fatalf("symlink: %v", err)
		}

		_, err := Bridge(HarnessCodex, "personal", vault, Options{DryRun: false})
		if err == nil {
			t.Errorf("expected error when symlink exists pointing elsewhere")
		}
		if err != nil && !strings.Contains(err.Error(), "already symlinked") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}
