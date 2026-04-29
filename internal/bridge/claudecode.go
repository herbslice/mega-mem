package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Claude Code memory layout:
//
//   ~/.claude/projects/<slug>/memory/MEMORY.md
//   ~/.claude/projects/<slug>/memory/<topic>.md
//
// The parent of `<slug>/memory/` is configurable via the autoMemoryDirectory
// setting in ~/.claude/settings.json (or .claude/settings.local.json or the
// managed-policy file — explicitly NOT project settings, by Claude Code's
// design).
//
// Setting autoMemoryDirectory: <vault>/agent-memory/claude-code/ causes
// future per-project memory dirs to be created inside the vault. The bridge
// command takes a <slug> argument so it can also migrate that specific
// project's existing memory dir into the vault. Other projects continue to
// land in the vault automatically once the redirect is in place.

const (
	claudeSettingsRel       = ".claude/settings.json"
	claudeProjectsRel       = ".claude/projects"
	claudeMemorySubdir      = "memory"
	autoMemoryDirectoryKey  = "autoMemoryDirectory"
	mcpServersKey           = "mcpServers"
	mcpServerName           = "mega-mem"
	claudeBridgeRootSegment = "agent-memory/claude-code"
)

func planBridgeClaudeCode(scope, vaultRoot string, opts Options) (*Result, error) {
	if scope == "" {
		return nil, fmt.Errorf("claude-code bridge requires a project slug (e.g., -home-user-work-myrepo)")
	}
	home, err := homeDir()
	if err != nil {
		return nil, err
	}

	settingsPath := filepath.Join(home, claudeSettingsRel)
	bridgeRoot := filepath.Join(vaultRoot, claudeBridgeRootSegment)
	defaultProjectMem := filepath.Join(home, claudeProjectsRel, scope, claudeMemorySubdir)
	vaultProjectMem := filepath.Join(bridgeRoot, scope, claudeMemorySubdir)

	res := &Result{Harness: HarnessClaudeCode, Scope: scope, DryRun: opts.DryRun}

	if !opts.SkipMemory {
		// Step 1: ensure the vault subdirectory exists.
		res.Steps = append(res.Steps, Step{
			Kind:        "mkdir",
			Description: fmt.Sprintf("ensure %s exists", vaultProjectMem),
			Apply: func() error {
				return os.MkdirAll(vaultProjectMem, 0o755)
			},
		})

		// Step 2: migrate existing memory if any.
		if dirExists(defaultProjectMem) && !isEmptyDir(defaultProjectMem) {
			res.Steps = append(res.Steps, Step{
				Kind:        "copy",
				Description: fmt.Sprintf("copy %s → %s", defaultProjectMem, vaultProjectMem),
				Apply: func() error {
					if err := copyTree(defaultProjectMem, vaultProjectMem); err != nil {
						return err
					}
					return os.RemoveAll(defaultProjectMem)
				},
			})
		}

		// Step 3: edit ~/.claude/settings.json to set autoMemoryDirectory.
		res.Steps = append(res.Steps, Step{
			Kind:        "settings-edit",
			Description: fmt.Sprintf("set autoMemoryDirectory in %s to %s", settingsPath, bridgeRoot),
			Apply: func() error {
				return setClaudeAutoMemoryDirectory(settingsPath, bridgeRoot)
			},
		})
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

	if !opts.DryRun {
		for _, st := range res.Steps {
			if st.Apply == nil {
				continue
			}
			if err := st.Apply(); err != nil {
				return res, fmt.Errorf("%s: %w", st.Description, err)
			}
			res.Executed++
		}
	}
	return res, nil
}

func planUnbridgeClaudeCode(scope, vaultRoot string, opts Options) (*Result, error) {
	if scope == "" {
		return nil, fmt.Errorf("claude-code unbridge requires a project slug")
	}
	home, err := homeDir()
	if err != nil {
		return nil, err
	}

	settingsPath := filepath.Join(home, claudeSettingsRel)
	bridgeRoot := filepath.Join(vaultRoot, claudeBridgeRootSegment)
	defaultProjectMem := filepath.Join(home, claudeProjectsRel, scope, claudeMemorySubdir)
	vaultProjectMem := filepath.Join(bridgeRoot, scope, claudeMemorySubdir)

	res := &Result{Harness: HarnessClaudeCode, Scope: scope, DryRun: opts.DryRun}

	if !opts.SkipMCP {
		res.Steps = append(res.Steps, Step{
			Kind:        "mcp-edit",
			Description: fmt.Sprintf("remove %q MCP server entry from %s", mcpServerName, settingsPath),
			Apply: func() error {
				return clearClaudeMCPServer(settingsPath, mcpServerName)
			},
		})
	}

	if !opts.SkipMemory {
		// Copy vault content back to default location.
		if dirExists(vaultProjectMem) && !isEmptyDir(vaultProjectMem) {
			res.Steps = append(res.Steps, Step{
				Kind:        "copy",
				Description: fmt.Sprintf("copy %s → %s", vaultProjectMem, defaultProjectMem),
				Apply: func() error {
					if err := os.MkdirAll(defaultProjectMem, 0o755); err != nil {
						return err
					}
					return copyTree(vaultProjectMem, defaultProjectMem)
				},
			})
		}

		// Remove autoMemoryDirectory from settings (only if it points
		// at our bridge root — don't clobber a user-set custom path).
		res.Steps = append(res.Steps, Step{
			Kind:        "settings-edit",
			Description: fmt.Sprintf("clear autoMemoryDirectory in %s if it equals %s", settingsPath, bridgeRoot),
			Apply: func() error {
				return clearClaudeAutoMemoryDirectory(settingsPath, bridgeRoot)
			},
		})

		// Optionally remove the vault subtree.
		if !opts.KeepVault {
			res.Steps = append(res.Steps, Step{
				Kind:        "rmdir",
				Description: fmt.Sprintf("remove %s (vault subtree; --keep-vault to preserve)", filepath.Join(bridgeRoot, scope)),
				Apply: func() error {
					return os.RemoveAll(filepath.Join(bridgeRoot, scope))
				},
			})
		}
	}

	if !opts.DryRun {
		for _, st := range res.Steps {
			if st.Apply == nil {
				continue
			}
			if err := st.Apply(); err != nil {
				return res, fmt.Errorf("%s: %w", st.Description, err)
			}
			res.Executed++
		}
	}
	return res, nil
}

// setClaudeAutoMemoryDirectory updates ~/.claude/settings.json to set
// autoMemoryDirectory. Preserves all other settings. Creates the file if
// missing. If the key is already set to a different value, returns an error
// rather than silently overwriting — the user should review.
func setClaudeAutoMemoryDirectory(settingsPath, value string) error {
	settings, err := readJSONObject(settingsPath)
	if err != nil {
		return err
	}
	if existing, ok := settings[autoMemoryDirectoryKey].(string); ok && existing != value {
		return fmt.Errorf("%s already sets %s=%q; refusing to overwrite", settingsPath, autoMemoryDirectoryKey, existing)
	}
	settings[autoMemoryDirectoryKey] = value
	return writeJSONObject(settingsPath, settings)
}

// clearClaudeAutoMemoryDirectory removes the autoMemoryDirectory key only
// if it equals the expected bridge root. If it points elsewhere, the user
// has set a custom value we shouldn't touch.
func clearClaudeAutoMemoryDirectory(settingsPath, expected string) error {
	settings, err := readJSONObject(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	current, ok := settings[autoMemoryDirectoryKey].(string)
	if !ok {
		return nil
	}
	if current != expected {
		return fmt.Errorf("%s sets %s=%q (expected %q); leaving untouched", settingsPath, autoMemoryDirectoryKey, current, expected)
	}
	delete(settings, autoMemoryDirectoryKey)
	return writeJSONObject(settingsPath, settings)
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
