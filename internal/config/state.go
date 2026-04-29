package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// State captures machine-local mega-mem runtime flags that don't belong to a
// specific vault (registry) or engine (per-vault server config). Lives at
// $XDG_CONFIG_HOME/mega-mem/state.yaml. Schema is intentionally narrow —
// add a new field rather than overloading existing ones.
type State struct {
	// HooksEnabled gates the mega-mem hook scripts (Claude Code, Codex,
	// future harnesses). When false, hooks read this file and exit 0
	// without injecting anything. Default true: a missing file or absent
	// field means hooks are active. Toggled by `mega-mem hooks
	// {enable,disable}`.
	HooksEnabled *bool `yaml:"hooks_enabled,omitempty"`
}

// HooksEnabledOrDefault returns true when the field is unset, preserving the
// "absent = enabled" semantics that hook scripts also implement.
func (s *State) HooksEnabledOrDefault() bool {
	if s == nil || s.HooksEnabled == nil {
		return true
	}
	return *s.HooksEnabled
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
// (which behaves as "all defaults" — see HooksEnabledOrDefault).
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

// SetHooksEnabled is a convenience that loads, mutates, and writes the
// state file in one call. Safe across concurrent invocations only at the
// granularity of "last writer wins"; mega-mem doesn't currently expect
// state mutations to race.
func SetHooksEnabled(enabled bool) error {
	s, err := LoadState()
	if err != nil {
		return err
	}
	s.HooksEnabled = &enabled
	return WriteState(s)
}
