package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withFakeXDG runs fn with $XDG_CONFIG_HOME pointed at a fresh tempdir,
// restoring the original env after.
func withFakeXDG(t *testing.T, fn func(xdg string)) {
	t.Helper()
	tmp := t.TempDir()
	orig, hadOrig := os.LookupEnv("XDG_CONFIG_HOME")
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Cleanup(func() {
		if hadOrig {
			os.Setenv("XDG_CONFIG_HOME", orig)
		} else {
			os.Unsetenv("XDG_CONFIG_HOME")
		}
	})
	fn(tmp)
}

func TestLoadState_MissingFileReturnsDefault(t *testing.T) {
	withFakeXDG(t, func(_ string) {
		s, err := LoadState()
		if err != nil {
			t.Fatalf("LoadState: %v", err)
		}
		for _, h := range KnownHarnesses {
			if !s.HooksEnabledForHarness(h) {
				t.Errorf("HooksEnabledForHarness(%q) = false on missing file, want true", h)
			}
		}
	})
}

func TestSetHooksEnabledForHarness_RoundTrip(t *testing.T) {
	withFakeXDG(t, func(xdg string) {
		if err := SetHooksEnabledForHarness("claude-code", false); err != nil {
			t.Fatalf("SetHooksEnabledForHarness(claude-code, false): %v", err)
		}
		s, err := LoadState()
		if err != nil {
			t.Fatalf("LoadState: %v", err)
		}
		if s.HooksEnabledForHarness("claude-code") {
			t.Errorf("claude-code = enabled after disable, want disabled")
		}
		// Other harnesses remain enabled (absent key = enabled).
		if !s.HooksEnabledForHarness("codex") {
			t.Errorf("codex = disabled, want enabled (absent key)")
		}
		// File should exist at the conventional path.
		want := filepath.Join(xdg, "mega-mem", "state.yaml")
		if _, err := os.Stat(want); err != nil {
			t.Errorf("state file %s not found: %v", want, err)
		}

		// Toggle back to enabled.
		if err := SetHooksEnabledForHarness("claude-code", true); err != nil {
			t.Fatalf("SetHooksEnabledForHarness(claude-code, true): %v", err)
		}
		s, err = LoadState()
		if err != nil {
			t.Fatalf("LoadState (re-enable): %v", err)
		}
		if !s.HooksEnabledForHarness("claude-code") {
			t.Errorf("claude-code = disabled after re-enable, want enabled")
		}
	})
}

func TestSetAllHooksEnabled(t *testing.T) {
	withFakeXDG(t, func(_ string) {
		if err := SetAllHooksEnabled(false); err != nil {
			t.Fatalf("SetAllHooksEnabled(false): %v", err)
		}
		s, err := LoadState()
		if err != nil {
			t.Fatalf("LoadState: %v", err)
		}
		for _, h := range KnownHarnesses {
			if s.HooksEnabledForHarness(h) {
				t.Errorf("%s = enabled after SetAllHooksEnabled(false), want disabled", h)
			}
		}
	})
}

func TestState_HooksEnabledForHarness_NilSafe(t *testing.T) {
	var s *State
	if !s.HooksEnabledForHarness("claude-code") {
		t.Errorf("nil receiver should return true (default), got false")
	}
}

func TestLegacyMigration(t *testing.T) {
	withFakeXDG(t, func(xdg string) {
		statePath := filepath.Join(xdg, "mega-mem", "state.yaml")
		if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		// Write the legacy v0.0.0 format manually.
		if err := os.WriteFile(statePath, []byte("hooks_enabled: false\n"), 0o644); err != nil {
			t.Fatalf("write legacy state: %v", err)
		}

		s, err := LoadState()
		if err != nil {
			t.Fatalf("LoadState: %v", err)
		}
		// Legacy false should disable every known harness.
		for _, h := range KnownHarnesses {
			if s.HooksEnabledForHarness(h) {
				t.Errorf("%s = enabled, want disabled (legacy false should propagate)", h)
			}
		}

		// Persist; legacy field should be gone, per-harness map should remain.
		if err := WriteState(s); err != nil {
			t.Fatalf("WriteState: %v", err)
		}
		data, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if strings.Contains(string(data), "hooks_enabled") {
			t.Errorf("legacy hooks_enabled key still in state file after migration: %q", data)
		}
		if !strings.Contains(string(data), "hooks:") {
			t.Errorf("new hooks: block missing after migration: %q", data)
		}
	})
}

// TestHookStateYAMLFormat verifies the on-disk layout matches what the
// shell hooks expect. The shipped hook scripts use awk to scan a `hooks:`
// block where each line is `    <harness>: <bool>`. Catches drift between
// the writer (Go) and reader (shell awk) at compile time of the test.
func TestHookStateYAMLFormat(t *testing.T) {
	withFakeXDG(t, func(_ string) {
		if err := SetHooksEnabledForHarness("codex", false); err != nil {
			t.Fatalf("SetHooksEnabledForHarness: %v", err)
		}
		path, err := StatePath()
		if err != nil {
			t.Fatalf("StatePath: %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		// Expected layout (yaml.v3 default 4-space indent):
		//   hooks:
		//       codex: false
		got := string(data)
		if !strings.Contains(got, "hooks:") {
			t.Errorf("missing top-level hooks block: %q", got)
		}
		// awk script in shipped hooks expects: ^[[:space:]]+codex:[[:space:]]*false[[:space:]]*$
		// Check at least one line satisfies that pattern.
		found := false
		for _, line := range strings.Split(got, "\n") {
			trimmed := strings.TrimLeft(line, " \t")
			if trimmed == "codex: false" && len(line) > len(trimmed) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected indented `codex: false` line in state file, got %q", got)
		}
	})
}
