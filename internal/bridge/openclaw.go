package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// OpenClaw memory layout:
//
//   ~/.openclaw/<workspace>/                <- workspace markdown + state
//     IDENTITY.md, SOUL.md, USER.md, ...    <- persona + project guidance
//     memory/<date>.md                      <- daily memory journal (BRIDGED)
//     state/, skills/, scripts/, ...        <- runtime state
//   ~/.openclaw/memory/<agent>.sqlite       <- index over the markdown
//
// The bridge is intentionally narrow: only the memory/ subdirectory of each
// workspace is redirected into the vault. Persona files (SOUL.md,
// IDENTITY.md, etc.), project guidance (AGENTS.md), and runtime state stay
// at their original paths under ~/.openclaw/<workspace>/.
//
// Multi-workspace fan-out: when Options.IncludeMemory is set with empty
// Scope, every workspace under ~/.openclaw/ is bridged. With a Scope,
// only that workspace is bridged. MCP wiring is independent and applies
// regardless of memory bridging.

const (
	openclawConfigRel = ".openclaw/openclaw.json"
	openclawHomeRel   = ".openclaw"
)

// openclawVaultMem returns the vault subdir for a given workspace.
//
//	<vault>/agent-memory/openclaw/<workspace>/
func openclawVaultMem(vaultRoot, workspace string) string {
	return filepath.Join(vaultRoot, "agent-memory", "openclaw", workspace)
}

// openclawWorkspaceMem returns the source memory dir for a workspace.
func openclawWorkspaceMem(home, workspace string) string {
	return filepath.Join(home, openclawHomeRel, workspace, "memory")
}

func planBridgeOpenClaw(vaultRoot string, opts Options) (*Result, error) {
	home, err := homeDir()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(home, openclawConfigRel)
	res := &Result{Harness: HarnessOpenClaw, Scope: opts.Scope, DryRun: opts.DryRun}

	if opts.IncludeMemory {
		workspaces, err := openclawWorkspacesToBridge(home, opts.Scope)
		if err != nil {
			return nil, err
		}
		for _, ws := range workspaces {
			defaultMem := openclawWorkspaceMem(home, ws)
			vaultMem := openclawVaultMem(vaultRoot, ws)
			steps, err := codexMemorySteps(defaultMem, vaultMem)
			if err != nil {
				return nil, fmt.Errorf("workspace %q: %w", ws, err)
			}
			res.Steps = append(res.Steps, steps...)
		}
	}

	if !opts.SkipMCP {
		res.Steps = append(res.Steps, Step{
			Kind:        "mcp-edit",
			Description: fmt.Sprintf("add mcp.servers.%s (url=%s) to %s", mcpServerName, opts.MCPURL, configPath),
			Apply: func() error {
				return setOpenClawMCPServer(configPath, mcpServerName, opts.MCPURL)
			},
		})
	}

	if err := applySteps(res, opts.DryRun); err != nil {
		return res, err
	}
	return res, nil
}

// openclawWorkspacesToBridge returns the workspaces to bridge based on Scope.
// scope == ""    → every dir under ~/.openclaw/ that has a memory/ child
//                  (or could grow one). Empty list = nothing to bridge.
// scope == "ws"  → []string{"ws"} unconditionally; bridge plan creates
//                  the workspace memory dir if missing.
func openclawWorkspacesToBridge(home, scope string) ([]string, error) {
	if scope != "" {
		return []string{scope}, nil
	}
	root := filepath.Join(home, openclawHomeRel)
	names, err := listDirNames(root)
	if err != nil {
		return nil, err
	}
	// Filter to workspaces that have a memory/ subdir or appear to be
	// workspace dirs (any dir at this level except the index dir "memory").
	var out []string
	for _, n := range names {
		if n == "memory" {
			continue // ~/.openclaw/memory/ is the global SQLite index
		}
		out = append(out, n)
	}
	return out, nil
}

func planUnbridgeOpenClaw(vaultRoot string, opts Options) (*Result, error) {
	home, err := homeDir()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(home, openclawConfigRel)
	res := &Result{Harness: HarnessOpenClaw, Scope: opts.Scope, DryRun: opts.DryRun}

	if !opts.SkipMCP {
		res.Steps = append(res.Steps, Step{
			Kind:        "mcp-edit",
			Description: fmt.Sprintf("remove mcp.servers.%s entry from %s", mcpServerName, configPath),
			Apply: func() error {
				return clearOpenClawMCPServer(configPath, mcpServerName)
			},
		})
	}

	if opts.IncludeMemory {
		workspaces, err := openclawWorkspacesToBridge(home, opts.Scope)
		if err != nil {
			return nil, err
		}
		for _, ws := range workspaces {
			defaultMem := openclawWorkspaceMem(home, ws)
			vaultMem := openclawVaultMem(vaultRoot, ws)

			if isSymlink(defaultMem) {
				target, err := os.Readlink(defaultMem)
				if err != nil {
					return nil, fmt.Errorf("workspace %q: read symlink %s: %w", ws, defaultMem, err)
				}
				if target != vaultMem {
					return nil, fmt.Errorf("workspace %q: %s symlinks to %s, not %s; refusing to unbridge", ws, defaultMem, target, vaultMem)
				}
			} else if dirExists(defaultMem) {
				return nil, fmt.Errorf("workspace %q: %s is a real directory (not a symlink); nothing to unbridge", ws, defaultMem)
			} else {
				continue // not bridged; skip
			}

			res.Steps = append(res.Steps, Step{
				Kind:        "unlink",
				Description: fmt.Sprintf("remove symlink %s", defaultMem),
				Apply:       func() error { return os.Remove(defaultMem) },
			})

			if dirExists(vaultMem) {
				dm, vm := defaultMem, vaultMem
				res.Steps = append(res.Steps, Step{
					Kind:        "copy",
					Description: fmt.Sprintf("copy %s → %s", vm, dm),
					Apply: func() error {
						if err := os.MkdirAll(dm, 0o755); err != nil {
							return err
						}
						return copyTree(vm, dm)
					},
				})
			}

			if !opts.KeepVault {
				vm := vaultMem
				res.Steps = append(res.Steps, Step{
					Kind:        "rmdir",
					Description: fmt.Sprintf("remove %s (vault subtree; --keep-vault to preserve)", vm),
					Apply:       func() error { return os.RemoveAll(vm) },
				})
			}
		}
	}

	if err := applySteps(res, opts.DryRun); err != nil {
		return res, err
	}
	return res, nil
}

// listOpenClawScopes enumerates workspaces under ~/.openclaw/ (excluding
// the index dir "memory").
func listOpenClawScopes() ([]string, error) {
	home, err := homeDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(home, openclawHomeRel)
	names, err := listDirNames(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range names {
		if n == "memory" {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}

// setOpenClawMCPServer adds mega-mem's MCP entry under mcp.servers in the
// OpenClaw config.
func setOpenClawMCPServer(configPath, name, url string) error {
	cfg, err := readJSONObject(configPath)
	if err != nil {
		return err
	}
	mcp := getOrCreateMap(cfg, "mcp")
	servers := getOrCreateMap(mcp, "servers")
	servers[name] = map[string]any{
		"url":       url,
		"transport": "http",
	}
	mcp["servers"] = servers
	cfg["mcp"] = mcp
	return writeJSONObjectPreservingFormat(configPath, cfg)
}

func clearOpenClawMCPServer(configPath, name string) error {
	cfg, err := readJSONObject(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	mcp, ok := cfg["mcp"].(map[string]any)
	if !ok {
		return nil
	}
	servers, ok := mcp["servers"].(map[string]any)
	if !ok {
		return nil
	}
	delete(servers, name)
	if len(servers) == 0 {
		delete(mcp, "servers")
	} else {
		mcp["servers"] = servers
	}
	if len(mcp) == 0 {
		delete(cfg, "mcp")
	} else {
		cfg["mcp"] = mcp
	}
	return writeJSONObjectPreservingFormat(configPath, cfg)
}

func getOrCreateMap(parent map[string]any, key string) map[string]any {
	if existing, ok := parent[key].(map[string]any); ok {
		return existing
	}
	created := map[string]any{}
	parent[key] = created
	return created
}

// writeJSONObjectPreservingFormat writes JSON with 2-space indent.
func writeJSONObjectPreservingFormat(path string, m map[string]any) error {
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
