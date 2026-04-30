package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Claude Code memory layout:
//
//   ~/.claude/projects/<slug>/                    <- per-project state
//     memory/MEMORY.md                            <- per-project memory
//     memory/<topic>.md                           <- per-project memory
//     todos/, messages/, ...                      <- other state
//
// With Options.IncludeMemory and empty Scope, Bridge symlinks the whole
// `~/.claude/projects/` directory into the vault. This captures every
// project's full state (memory plus everything else) and ensures new
// project folders Claude Code creates after bridging end up inside the
// vault automatically — no race window.
//
// With a non-empty Scope (a project slug like `-home-user-work-foo`),
// Bridge narrows to that one project's memory subdir, symlinking
// ~/.claude/projects/<slug>/memory/ into the vault. The rest of the
// project's state stays at its original location. Useful for users
// piloting the bridge on a single project before committing to whole-dir.
//
// Without IncludeMemory (the default), Bridge only adds mega-mem to
// ~/.claude/settings.json's mcpServers map. No filesystem moves; trivially
// reversible.

const (
	claudeSettingsRel       = ".claude/settings.json"
	claudeProjectsRel       = ".claude/projects"
	claudeMemorySubdir      = "memory"
	mcpServersKey           = "mcpServers"
	mcpServerName           = "mega-mem"
	claudeBridgeRootSegment = "agent-memory/claude-code/projects"
)

// claudeProjectsVaultPath returns <vault>/agent-memory/claude-code/projects/.
func claudeProjectsVaultPath(vaultRoot string) string {
	return filepath.Join(vaultRoot, claudeBridgeRootSegment)
}

// claudeProjectMemVaultPath returns the vault subdir for one project's
// memory: <vault>/agent-memory/claude-code/projects/<slug>/memory/.
func claudeProjectMemVaultPath(vaultRoot, slug string) string {
	return filepath.Join(vaultRoot, claudeBridgeRootSegment, slug, claudeMemorySubdir)
}

func planBridgeClaudeCode(vaultRoot string, opts Options) (*Result, error) {
	home, err := homeDir()
	if err != nil {
		return nil, err
	}

	settingsPath := filepath.Join(home, claudeSettingsRel)
	res := &Result{Harness: HarnessClaudeCode, Scope: opts.Scope, DryRun: opts.DryRun}

	if opts.IncludeMemory {
		var steps []Step
		if opts.Scope == "" {
			// Whole-projects-dir mode.
			defaultProj := filepath.Join(home, claudeProjectsRel)
			vaultProj := claudeProjectsVaultPath(vaultRoot)
			steps, err = codexMemorySteps(defaultProj, vaultProj)
		} else {
			// Single-project memory mode.
			defaultMem := filepath.Join(home, claudeProjectsRel, opts.Scope, claudeMemorySubdir)
			vaultMem := claudeProjectMemVaultPath(vaultRoot, opts.Scope)
			steps, err = codexMemorySteps(defaultMem, vaultMem)
		}
		if err != nil {
			return nil, err
		}
		res.Steps = append(res.Steps, steps...)
	}

	if !opts.SkipMCP {
		res.Steps = append(res.Steps, Step{
			Kind:        "mcp-edit",
			Description: fmt.Sprintf("add %q MCP server (url=%s) to %s", mcpServerName, opts.MCPURL, settingsPath),
			Apply: func() error {
				return setClaudeMCPServer(settingsPath, mcpServerName, opts.MCPURL)
			},
		})
	}

	if err := applySteps(res, opts.DryRun); err != nil {
		return res, err
	}
	return res, nil
}

func planUnbridgeClaudeCode(vaultRoot string, opts Options) (*Result, error) {
	home, err := homeDir()
	if err != nil {
		return nil, err
	}

	settingsPath := filepath.Join(home, claudeSettingsRel)
	res := &Result{Harness: HarnessClaudeCode, Scope: opts.Scope, DryRun: opts.DryRun}

	if !opts.SkipMCP {
		res.Steps = append(res.Steps, Step{
			Kind:        "mcp-edit",
			Description: fmt.Sprintf("remove %q MCP server entry from %s", mcpServerName, settingsPath),
			Apply: func() error {
				return clearClaudeMCPServer(settingsPath, mcpServerName)
			},
		})
	}

	if opts.IncludeMemory {
		var defaultPath, vaultPath string
		if opts.Scope == "" {
			defaultPath = filepath.Join(home, claudeProjectsRel)
			vaultPath = claudeProjectsVaultPath(vaultRoot)
		} else {
			defaultPath = filepath.Join(home, claudeProjectsRel, opts.Scope, claudeMemorySubdir)
			vaultPath = claudeProjectMemVaultPath(vaultRoot, opts.Scope)
		}

		if isSymlink(defaultPath) {
			target, err := os.Readlink(defaultPath)
			if err != nil {
				return nil, fmt.Errorf("read symlink %s: %w", defaultPath, err)
			}
			if target != vaultPath {
				return nil, fmt.Errorf("%s symlinks to %s, not %s; refusing to unbridge", defaultPath, target, vaultPath)
			}
		} else if dirExists(defaultPath) {
			return nil, fmt.Errorf("%s is a real directory (not a symlink); nothing to unbridge", defaultPath)
		}

		res.Steps = append(res.Steps, Step{
			Kind:        "unlink",
			Description: fmt.Sprintf("remove symlink %s", defaultPath),
			Apply:       func() error { return os.Remove(defaultPath) },
		})

		if dirExists(vaultPath) {
			res.Steps = append(res.Steps, Step{
				Kind:        "copy",
				Description: fmt.Sprintf("copy %s → %s", vaultPath, defaultPath),
				Apply: func() error {
					if err := os.MkdirAll(defaultPath, 0o755); err != nil {
						return err
					}
					return copyTree(vaultPath, defaultPath)
				},
			})
		}

		if !opts.KeepVault {
			res.Steps = append(res.Steps, Step{
				Kind:        "rmdir",
				Description: fmt.Sprintf("remove %s (vault subtree; --keep-vault to preserve)", vaultPath),
				Apply:       func() error { return os.RemoveAll(vaultPath) },
			})
		}
	}

	if err := applySteps(res, opts.DryRun); err != nil {
		return res, err
	}
	return res, nil
}

// listClaudeScopes enumerates project slugs under ~/.claude/projects/.
// Returns the dir basenames (slugs).
func listClaudeScopes() ([]string, error) {
	home, err := homeDir()
	if err != nil {
		return nil, err
	}
	return listDirNames(filepath.Join(home, claudeProjectsRel))
}

// setClaudeMCPServer adds or updates a single mcpServers entry in the
// Claude Code settings file. The new entry uses URL-only form
// (`{"url": "..."}`), which Claude Code accepts for SSE-transport servers.
// Other servers in mcpServers are preserved.
func setClaudeMCPServer(settingsPath, name, url string) error {
	settings, err := readJSONObject(settingsPath)
	if err != nil {
		return err
	}
	servers := getOrCreateMap(settings, mcpServersKey)
	servers[name] = map[string]any{"url": url}
	settings[mcpServersKey] = servers
	return writeJSONObject(settingsPath, settings)
}

// clearClaudeMCPServer removes a single mcpServers entry by name. If the
// entry doesn't exist, no-op. Other servers are preserved.
func clearClaudeMCPServer(settingsPath, name string) error {
	settings, err := readJSONObject(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	servers, ok := settings[mcpServersKey].(map[string]any)
	if !ok {
		return nil
	}
	delete(servers, name)
	if len(servers) == 0 {
		delete(settings, mcpServersKey)
	} else {
		settings[mcpServersKey] = servers
	}
	return writeJSONObject(settingsPath, settings)
}

// readJSONObject reads a JSON object from path. Missing file returns an
// empty map. Non-object top-level value returns an error.
func readJSONObject(path string) (map[string]any, error) {
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
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// writeJSONObject writes a JSON object back to path with 2-space indent and
// a trailing newline. Creates parents as needed.
func writeJSONObject(path string, m map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
