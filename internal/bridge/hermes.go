package bridge

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Hermes memory layout:
//
//   ~/.hermes/memories/MEMORY.md   (single file, entries delimited by "\n§\n")
//   ~/.hermes/memories/USER.md     (separate file for user facts)
//   ~/.hermes/memories/MEMORY.md.lock
//
// Hermes writes via atomic temp-file + os.replace, which is safe within a
// directory but not across symlinked file paths. Bridge therefore symlinks
// the memories *directory* (not individual files) so renames stay inside
// the symlinked target. Same pattern as Codex.
//
// Hermes config (~/.hermes/config.yaml) supports a top-level mcp_servers
// dict in the same shape as Claude Code's mcpServers. Bridge adds a
// mega-mem entry pointing at the SSE URL. Memory bridging is opt-in via
// Options.IncludeMemory.

const (
	hermesMemoriesRel = ".hermes/memories"
	hermesConfigRel   = ".hermes/config.yaml"
	hermesDefaultPool = "memories"
)

// hermesVaultMem returns the vault subdir for a given scope.
func hermesVaultMem(vaultRoot, scope string) string {
	if scope == "" {
		scope = hermesDefaultPool
	}
	return filepath.Join(vaultRoot, "agent-memory", "hermes", scope)
}

func planBridgeHermes(vaultRoot string, opts Options) (*Result, error) {
	home, err := homeDir()
	if err != nil {
		return nil, err
	}

	defaultMem := filepath.Join(home, hermesMemoriesRel)
	configPath := filepath.Join(home, hermesConfigRel)
	vaultMem := hermesVaultMem(vaultRoot, opts.Scope)

	res := &Result{Harness: HarnessHermes, Scope: opts.Scope, DryRun: opts.DryRun}

	if opts.IncludeMemory {
		steps, err := codexMemorySteps(defaultMem, vaultMem) // identical pattern
		if err != nil {
			return nil, err
		}
		res.Steps = append(res.Steps, steps...)
	}

	if !opts.SkipMCP {
		res.Steps = append(res.Steps, Step{
			Kind:        "mcp-edit",
			Description: fmt.Sprintf("add mcp_servers.%s (url=%s) to %s", mcpServerName, opts.MCPURL, configPath),
			Apply: func() error {
				return setHermesMCPServer(configPath, mcpServerName, opts.MCPURL)
			},
		})
	}

	if err := applySteps(res, opts.DryRun); err != nil {
		return res, err
	}
	return res, nil
}

func planUnbridgeHermes(vaultRoot string, opts Options) (*Result, error) {
	home, err := homeDir()
	if err != nil {
		return nil, err
	}

	defaultMem := filepath.Join(home, hermesMemoriesRel)
	configPath := filepath.Join(home, hermesConfigRel)
	vaultMem := hermesVaultMem(vaultRoot, opts.Scope)

	res := &Result{Harness: HarnessHermes, Scope: opts.Scope, DryRun: opts.DryRun}

	if !opts.SkipMCP {
		res.Steps = append(res.Steps, Step{
			Kind:        "mcp-edit",
			Description: fmt.Sprintf("remove mcp_servers.%s entry from %s", mcpServerName, configPath),
			Apply: func() error {
				return clearHermesMCPServer(configPath, mcpServerName)
			},
		})
	}

	if opts.IncludeMemory {
		if isSymlink(defaultMem) {
			target, err := os.Readlink(defaultMem)
			if err != nil {
				return nil, fmt.Errorf("read symlink %s: %w", defaultMem, err)
			}
			if target != vaultMem {
				return nil, fmt.Errorf("%s symlinks to %s, not %s; refusing to unbridge a different bridge", defaultMem, target, vaultMem)
			}
		} else if dirExists(defaultMem) {
			return nil, fmt.Errorf("%s is a real directory (not a symlink); nothing to unbridge", defaultMem)
		}

		res.Steps = append(res.Steps, Step{
			Kind:        "unlink",
			Description: fmt.Sprintf("remove symlink %s", defaultMem),
			Apply:       func() error { return os.Remove(defaultMem) },
		})

		if dirExists(vaultMem) {
			res.Steps = append(res.Steps, Step{
				Kind:        "copy",
				Description: fmt.Sprintf("copy %s → %s", vaultMem, defaultMem),
				Apply: func() error {
					if err := os.MkdirAll(defaultMem, 0o755); err != nil {
						return err
					}
					return copyTree(vaultMem, defaultMem)
				},
			})
		}

		if !opts.KeepVault {
			res.Steps = append(res.Steps, Step{
				Kind:        "rmdir",
				Description: fmt.Sprintf("remove %s (vault subtree; --keep-vault to preserve)", vaultMem),
				Apply:       func() error { return os.RemoveAll(vaultMem) },
			})
		}
	}

	if err := applySteps(res, opts.DryRun); err != nil {
		return res, err
	}
	return res, nil
}

// setHermesMCPServer adds or updates the named entry under top-level
// mcp_servers in Hermes's config.yaml. Other entries are preserved. The
// config-yaml format supports either url-form ({url: "..."}) or
// command-form ({command: ..., args: [...]}); we use url-form to match
// mega-mem's SSE transport.
func setHermesMCPServer(configPath, name, url string) error {
	cfg, err := readYAMLMap(configPath)
	if err != nil {
		return err
	}
	servers := getOrCreateMap(cfg, "mcp_servers")
	servers[name] = map[string]any{"url": url}
	cfg["mcp_servers"] = servers
	return writeYAMLMap(configPath, cfg)
}

func clearHermesMCPServer(configPath, name string) error {
	cfg, err := readYAMLMap(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	servers, ok := cfg["mcp_servers"].(map[string]any)
	if !ok {
		return nil
	}
	delete(servers, name)
	if len(servers) == 0 {
		delete(cfg, "mcp_servers")
	} else {
		cfg["mcp_servers"] = servers
	}
	return writeYAMLMap(configPath, cfg)
}

func readYAMLMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func writeYAMLMap(path string, m map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
