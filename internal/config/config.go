// Package config defines the three config layers used by mega-mem:
//
//   - Engine: machine-local, per-vault engine runtime (bind, embedding, agent-memory paths).
//     Lives in $XDG_CONFIG_HOME/mega-mem/engines/<alias>.yaml.
//
//   - Vault: in-vault config that travels with the vault across machines.
//     Lives in <vault>/.mega-mem.yaml.
//
//   - Registry: vault alias → path mapping. Machine-local, non-syncing.
//     Lives in $XDG_CONFIG_HOME/mega-mem/vaults.yaml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Engine is the machine-local config controlling a single mega-mem process.
type Engine struct {
	VaultPath   string        `yaml:"vault_path"`
	Bind        string        `yaml:"bind"`
	Embedding   EmbeddingCfg  `yaml:"embedding"`
	AgentMemory []AgentMemory `yaml:"agent_memory,omitempty"`
	LogLevel    string        `yaml:"log_level,omitempty"`
}

// EmbeddingCfg describes how to reach the embedding provider.
type EmbeddingCfg struct {
	Provider string `yaml:"provider"`
	Endpoint string `yaml:"endpoint"`
	Model    string `yaml:"model"`
	APIKey   string `yaml:"api_key,omitempty"`
}

// AgentMemory declares one harness-native memory store to mount under agent-memory/.
type AgentMemory struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
}

// Vault is the in-vault config.
type Vault struct {
	VaultID string    `yaml:"vault_id"`
	Commit  CommitCfg `yaml:"commit,omitempty"`
}

// CommitCfg controls the git commit engine (v1.x feature; fields reserved).
type CommitCfg struct {
	DebounceMinutes int    `yaml:"debounce_minutes,omitempty"`
	Remote          string `yaml:"remote,omitempty"`
}

// Registry is the machine-local alias → path mapping.
type Registry struct {
	Vaults map[string]RegistryEntry `yaml:"vaults"`
}

// RegistryEntry is one registered vault.
type RegistryEntry struct {
	Path string `yaml:"path"`
}

// LoadEngine reads and validates an engine config from disk.
func LoadEngine(path string) (*Engine, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read engine config %s: %w", path, err)
	}
	e := &Engine{
		Bind:     "127.0.0.1:8111",
		LogLevel: "info",
	}
	if err := yaml.Unmarshal(data, e); err != nil {
		return nil, fmt.Errorf("parse engine config %s: %w", path, err)
	}
	if e.VaultPath == "" {
		return nil, fmt.Errorf("engine config %s: vault_path is required", path)
	}
	absVault, err := filepath.Abs(e.VaultPath)
	if err != nil {
		return nil, fmt.Errorf("resolve vault_path: %w", err)
	}
	e.VaultPath = absVault
	info, err := os.Stat(absVault)
	if err != nil {
		return nil, fmt.Errorf("vault_path %s: %w", absVault, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("vault_path %s is not a directory", absVault)
	}
	return e, nil
}

// EngineConfigPathForAlias returns the conventional engine config path for a
// given vault alias: $XDG_CONFIG_HOME/mega-mem/engines/<alias>.yaml.
func EngineConfigPathForAlias(alias string) (string, error) {
	dir, err := xdgConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mega-mem", "engines", alias+".yaml"), nil
}

// LoadVault reads a vault's in-vault config. If missing, returns a default.
func LoadVault(vaultPath string) (*Vault, error) {
	path := filepath.Join(vaultPath, ".mega-mem.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Vault{VaultID: filepath.Base(vaultPath)}, nil
		}
		return nil, fmt.Errorf("read vault config %s: %w", path, err)
	}
	v := &Vault{}
	if err := yaml.Unmarshal(data, v); err != nil {
		return nil, fmt.Errorf("parse vault config %s: %w", path, err)
	}
	if v.VaultID == "" {
		v.VaultID = filepath.Base(vaultPath)
	}
	return v, nil
}

// WriteVault persists a vault's config to <vault>/.mega-mem.yaml.
func WriteVault(vaultPath string, v *Vault) error {
	path := filepath.Join(vaultPath, ".mega-mem.yaml")
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal vault config: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// RegistryPath returns $XDG_CONFIG_HOME/mega-mem/vaults.yaml.
func RegistryPath() (string, error) {
	dir, err := xdgConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mega-mem", "vaults.yaml"), nil
}

// LoadRegistry reads the vault registry. Missing file returns an empty registry.
func LoadRegistry() (*Registry, error) {
	path, err := RegistryPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Registry{Vaults: map[string]RegistryEntry{}}, nil
		}
		return nil, fmt.Errorf("read registry %s: %w", path, err)
	}
	r := &Registry{}
	if err := yaml.Unmarshal(data, r); err != nil {
		return nil, fmt.Errorf("parse registry %s: %w", path, err)
	}
	if r.Vaults == nil {
		r.Vaults = map[string]RegistryEntry{}
	}
	return r, nil
}

// WriteRegistry persists the registry.
func WriteRegistry(r *Registry) error {
	path, err := RegistryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create registry dir: %w", err)
	}
	data, err := yaml.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// ResolveRef turns a vault alias into an absolute path via the registry.
// Path-like refs are rejected: all vaults must be registered first.
func ResolveRef(ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("empty vault ref")
	}
	if strings.ContainsAny(ref, "/\\.") {
		return "", fmt.Errorf("invalid vault alias %q: aliases must not contain slashes or dots — register a path first via `mega-mem vaults register <alias> [<path>]`", ref)
	}
	reg, err := LoadRegistry()
	if err != nil {
		return "", err
	}
	entry, ok := reg.Vaults[ref]
	if !ok {
		return "", fmt.Errorf("vault alias %q not registered (try `mega-mem vaults list`)", ref)
	}
	return entry.Path, nil
}

// DefaultVaultPath returns the conventional filesystem path for a vault alias
// when no explicit path is given to `vaults register`:
// $XDG_DATA_HOME/mega-mem/vaults/<alias>/ (typically
// ~/.local/share/mega-mem/vaults/<alias>/).
func DefaultVaultPath(alias string) (string, error) {
	dir, err := xdgDataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mega-mem", "vaults", alias), nil
}

func xdgConfigHome() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("XDG_CONFIG_HOME unset and home dir unknown: %w", err)
	}
	return filepath.Join(home, ".config"), nil
}

func xdgDataHome() (string, error) {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("XDG_DATA_HOME unset and home dir unknown: %w", err)
	}
	return filepath.Join(home, ".local", "share"), nil
}
