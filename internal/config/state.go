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
// recipes and bridge logic for. Used by the `agents hooks` --all flag.
// Kept in sync with bridge.SupportedHarnesses(); a test in the bridge
// package verifies the alignment.
var KnownHarnesses = []string{"claude-code", "codex", "hermes", "openclaw"}

// State captures machine-local mega-mem runtime flags that don't belong to a
// specific vault (registry) or engine (per-vault server config). Lives at
// $XDG_CONFIG_HOME/mega-mem/state.yaml. Schema is intentionally narrow —
// add a new field rather than overloading existing ones.
type State struct {
	// HooksEnabled is the global kill switch. When set to false, every
	// harness's hooks are disabled regardless of the per-harness Hooks
	// map. When unset (nil) or true, the per-harness map decides.
	// Hand-edit only — not currently exposed in the CLI; reach for it
	// when you want to silence all mega-mem injection in one line, or
	// to cover harnesses added later without updating the per-harness
	// map.
	HooksEnabled *bool `yaml:"hooks_enabled,omitempty"`

	// Hooks gates per-harness mega-mem hook scripts. Map key is the
	// canonical harness name. Absent key (or whole map nil) means hooks
	// are enabled — fail-open default. Toggle via
	// `mega-mem agents hooks {enable,disable} [<harness>]`.
	Hooks map[string]bool `yaml:"hooks,omitempty"`
}

// HooksEnabledForHarness returns true when mega-mem's hook scripts for
// the named harness should run. Precedence: HooksEnabled=false acts as a
// kill switch and disables every harness; otherwise the per-harness Hooks
// map decides (absent key = enabled, fail-open). nil receiver = enabled.
func (s *State) HooksEnabledForHarness(harness string) bool {
	if s == nil {
		return true
	}
	if s.HooksEnabled != nil && !*s.HooksEnabled {
		return false
	}
	if v, ok := s.Hooks[harness]; ok {
		return v
	}
	return true
}

// KillSwitchActive reports whether the global hooks_enabled flag is
// explicitly set to false. Useful for CLI status output that wants to
// surface "all harnesses off because of a kill switch" vs. "all harnesses
// individually disabled."
func (s *State) KillSwitchActive() bool {
	return s != nil && s.HooksEnabled != nil && !*s.HooksEnabled
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
// (which behaves as "all defaults" — every harness enabled, no kill
// switch).
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
