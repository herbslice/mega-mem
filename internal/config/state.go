package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// KnownHarnesses lists the canonical harness names mega-mem ships hook
// recipes and bridge logic for. Used by state migration and by the
// `agents hooks` --all flag. Kept in sync with bridge.SupportedHarnesses();
// a test in the bridge package verifies the alignment.
var KnownHarnesses = []string{"claude-code", "codex", "hermes", "openclaw"}

// State captures machine-local mega-mem runtime flags that don't belong to a
// specific vault (registry) or engine (per-vault server config). Lives at
// $XDG_CONFIG_HOME/mega-mem/state.yaml. Schema is intentionally narrow —
// add a new field rather than overloading existing ones.
type State struct {
	// Hooks gates per-harness mega-mem hook scripts. Map key is the
	// canonical harness name. Absent key (or whole map nil) means hooks
	// are enabled — fail-open default. Toggle via
	// `mega-mem agents hooks {enable,disable} [<harness>]`.
	Hooks map[string]bool `yaml:"hooks,omitempty"`

	// LegacyHooksEnabled is the v0.0.0 top-level toggle. Read for
	// backward compatibility; migrated into Hooks on first WriteState.
	// Never written (omitempty + nil after LoadState completes migration).
	LegacyHooksEnabled *bool `yaml:"hooks_enabled,omitempty"`
}

// HooksEnabledForHarness returns true when mega-mem's hook scripts for
// the named harness should run. Absent key = enabled (fail-open).
// nil receiver = enabled.
func (s *State) HooksEnabledForHarness(harness string) bool {
	if s == nil {
		return true
	}
	if v, ok := s.Hooks[harness]; ok {
		return v
	}
	if s.LegacyHooksEnabled != nil {
		return *s.LegacyHooksEnabled
	}
	return true
}

// StatePath returns $XDG_CONFIG_HOME/mega-mem/state.yaml.
func StatePath() (string, error) {
	dir, err := xdgConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mega-mem", "state.yaml"), nil
}

// LoadState reads the state file. Missing file returns a zero-value State
// (which behaves as "all defaults"). Migrates the legacy `hooks_enabled`
// field into the per-harness `Hooks` map on read; subsequent WriteState
// calls persist the new schema and drop the legacy key.
func LoadState() (*State, error) {
	path, err := StatePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &State{}, nil
		}
		return nil, fmt.Errorf("read state %s: %w", path, err)
	}
	s := &State{}
	if err := yaml.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parse state %s: %w", path, err)
	}
	if s.LegacyHooksEnabled != nil && len(s.Hooks) == 0 {
		s.Hooks = make(map[string]bool, len(KnownHarnesses))
		for _, h := range KnownHarnesses {
			s.Hooks[h] = *s.LegacyHooksEnabled
		}
	}
	s.LegacyHooksEnabled = nil
	return s, nil
}

// WriteState persists state to disk, creating parents as needed.
func WriteState(s *State) error {
	path, err := StatePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// SetHooksEnabledForHarness loads, mutates, and writes the state file in
// one call. Safe across concurrent invocations only at the granularity of
// "last writer wins"; mega-mem doesn't currently expect state mutations
// to race.
func SetHooksEnabledForHarness(harness string, enabled bool) error {
	s, err := LoadState()
	if err != nil {
		return err
	}
	if s.Hooks == nil {
		s.Hooks = map[string]bool{}
	}
	s.Hooks[harness] = enabled
	return WriteState(s)
}

// SetAllHooksEnabled sets every known harness's hook flag at once.
func SetAllHooksEnabled(enabled bool) error {
	s, err := LoadState()
	if err != nil {
		return err
	}
	if s.Hooks == nil {
		s.Hooks = map[string]bool{}
	}
	for _, h := range KnownHarnesses {
		s.Hooks[h] = enabled
	}
	return WriteState(s)
}

// HookStatus is one row of the per-harness status report.
type HookStatus struct {
	Harness string
	Enabled bool
}

// AllHookStatuses returns the enabled flag for every known harness, in
// stable order. Useful for `agents hooks status`.
func (s *State) AllHookStatuses() []HookStatus {
	out := make([]HookStatus, 0, len(KnownHarnesses))
	names := append([]string(nil), KnownHarnesses...)
	sort.Strings(names)
	for _, h := range names {
		out = append(out, HookStatus{Harness: h, Enabled: s.HooksEnabledForHarness(h)})
	}
	return out
}
