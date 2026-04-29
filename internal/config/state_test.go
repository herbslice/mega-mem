package config

import (
	"os"
	"path/filepath"
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
		if !s.HooksEnabledOrDefault() {
			t.Errorf("HooksEnabledOrDefault() = false on missing file, want true")
		}
	})
}

func TestSetHooksEnabled_RoundTrip(t *testing.T) {
	withFakeXDG(t, func(xdg string) {
		if err := SetHooksEnabled(false); err != nil {
			t.Fatalf("SetHooksEnabled(false): %v", err)
		}
		s, err := LoadState()
		if err != nil {
			t.Fatalf("LoadState: %v", err)
		}
		if s.HooksEnabledOrDefault() {
			t.Errorf("HooksEnabledOrDefault() = true after disable, want false")
		}
		// File should exist at the conventional path.
		want := filepath.Join(xdg, "mega-mem", "state.yaml")
		if _, err := os.Stat(want); err != nil {
			t.Errorf("state file %s not found: %v", want, err)
		}

		// Toggle back to enabled.
		if err := SetHooksEnabled(true); err != nil {
			t.Fatalf("SetHooksEnabled(true): %v", err)
		}
		s, err = LoadState()
		if err != nil {
			t.Fatalf("LoadState (re-enable): %v", err)
		}
		if !s.HooksEnabledOrDefault() {
			t.Errorf("HooksEnabledOrDefault() = false after re-enable, want true")
		}
	})
}

func TestState_HooksEnabledOrDefault_NilSafe(t *testing.T) {
	var s *State
	if !s.HooksEnabledOrDefault() {
		t.Errorf("nil receiver should return true (default), got false")
	}
}

// TestStateFileMatchesHookGuard verifies the on-disk YAML format produced
// by SetHooksEnabled(false) matches the regex the hook scripts use to
// detect the disabled state. Catches drift between the writer (Go) and
// reader (shell grep) at compile time of the test.
func TestStateFileMatchesHookGuard(t *testing.T) {
	withFakeXDG(t, func(_ string) {
		if err := SetHooksEnabled(false); err != nil {
			t.Fatalf("SetHooksEnabled: %v", err)
		}
		path, err := StatePath()
		if err != nil {
			t.Fatalf("StatePath: %v", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read state file: %v", err)
		}
		// The hook scripts use this exact ERE: ^hooks_enabled:\s*false\s*$
		// (with optional whitespace). The yaml encoder produces
		// "hooks_enabled: false\n" — bare key, single space, no trailing space.
		// Verify the produced string matches.
		want := "hooks_enabled: false\n"
		if string(data) != want {
			t.Errorf("state file contents = %q, want %q (drift between Go writer and hook-script grep)", string(data), want)
		}
	})
}
