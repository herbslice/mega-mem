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

// TestKillSwitch_DisablesAllHarnesses verifies that hand-editing a global
// `hooks_enabled: false` disables every known harness regardless of the
// per-harness map.
func TestKillSwitch_DisablesAllHarnesses(t *testing.T) {
	withFakeXDG(t, func(xdg string) {
		statePath := filepath.Join(xdg, "mega-mem", "state.yaml")
		if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		// Hand-edited state: kill switch on, with one harness explicitly
		// enabled to confirm the kill switch wins.
		body := "hooks_enabled: false\nhooks:\n    codex: true\n"
		if err := os.WriteFile(statePath, []byte(body), 0o644); err != nil {
			t.Fatalf("write state: %v", err)
		}

		s, err := LoadState()
		if err != nil {
			t.Fatalf("LoadState: %v", err)
		}
		if !s.KillSwitchActive() {
			t.Errorf("KillSwitchActive() = false, want true")
		}
		for _, h := range KnownHarnesses {
			if s.HooksEnabledForHarness(h) {
				t.Errorf("%s = enabled, want disabled (kill switch should win over per-harness)", h)
			}
		}
	})
}

// TestKillSwitch_PersistsThroughWrite verifies that the global flag is a
// peer field and survives a load → write round-trip — there's no
// migration that drops it.
func TestKillSwitch_PersistsThroughWrite(t *testing.T) {
	withFakeXDG(t, func(xdg string) {
		statePath := filepath.Join(xdg, "mega-mem", "state.yaml")
		if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(statePath, []byte("hooks_enabled: false\n"), 0o644); err != nil {
			t.Fatalf("write state: %v", err)
		}

		// Round-trip via the CLI's per-harness toggle — should not touch
		// the global flag.
		if err := SetHooksEnabledForHarness("codex", true); err != nil {
			t.Fatalf("SetHooksEnabledForHarness: %v", err)
		}
		data, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if !strings.Contains(string(data), "hooks_enabled: false") {
			t.Errorf("global hooks_enabled flag dropped after CLI write; got %q", data)
		}
	})
}

// TestKillSwitch_TrueIsNoop verifies that hooks_enabled: true is
// equivalent to absent — per-harness map decides, fail-open default.
func TestKillSwitch_TrueIsNoop(t *testing.T) {
	withFakeXDG(t, func(xdg string) {
		statePath := filepath.Join(xdg, "mega-mem", "state.yaml")
		if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		body := "hooks_enabled: true\nhooks:\n    codex: false\n"
		if err := os.WriteFile(statePath, []byte(body), 0o644); err != nil {
			t.Fatalf("write state: %v", err)
		}

		s, err := LoadState()
		if err != nil {
			t.Fatalf("LoadState: %v", err)
		}
		if s.KillSwitchActive() {
			t.Errorf("KillSwitchActive() = true with hooks_enabled: true, want false")
		}
		if s.HooksEnabledForHarness("codex") {
			t.Errorf("codex = enabled, want disabled (per-harness false)")
		}
		if !s.HooksEnabledForHarness("claude-code") {
			t.Errorf("claude-code = disabled, want enabled (absent key, fail-open)")
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
