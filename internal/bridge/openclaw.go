package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// OpenClaw memory layout (per the active install on this machine):
//
//   ~/.openclaw/<workspace>/                <- workspace markdown + state
//     IDENTITY.md, SOUL.md, USER.md, ...    <- persona + project guidance
//     memory/<date>.md                      <- daily memory journal (BRIDGED)
//     state/, skills/, scripts/, ...        <- runtime state
//   ~/.openclaw/memory/<agent>.sqlite       <- index over the markdown
//
// The bridge is intentionally narrow: only the memory/ subdirectory is
// redirected into the vault. Persona files (SOUL.md, IDENTITY.md, etc.),
// project guidance (AGENTS.md), and runtime state stay at their original
// paths under ~/.openclaw/<workspace>/. Cross-machine portability for
// those is out of scope — see docs/SYNC-SUGGESTIONS.md for how to handle
// them with Syncthing or similar if desired.
//
// MCP wiring is independent: bridge adds mega-mem to mcp.servers in
// ~/.openclaw/openclaw.json regardless of memory bridging.

const (
	openclawConfigRel = ".openclaw/openclaw.json"
)

func planBridgeOpenClaw(scope, vaultRoot string, opts Options) (*Result, error) {
	if scope == "" {
		return nil, fmt.Errorf("openclaw bridge requires a workspace name")
	}
	home, err := homeDir()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(home, openclawConfigRel)
	defaultMem := filepath.Join(home, ".openclaw", scope, "memory")
	vaultMem := vaultSubdir(vaultRoot, HarnessOpenClaw, scope)

	res := &Result{Harness: HarnessOpenClaw, Scope: scope, DryRun: opts.DryRun}

	if !opts.SkipMemory {
		if isSymlink(defaultMem) {
			target, err := os.Readlink(defaultMem)
			if err != nil {
				return nil, fmt.Errorf("read existing symlink %s: %w", defaultMem, err)
			}
			if target != vaultMem {
				return nil, fmt.Errorf("%s already symlinked to %s; remove it before bridging", defaultMem, target)
			}
			res.Steps = append(res.Steps, Step{
				Kind:        "noop",
				Description: fmt.Sprintf("%s already symlinked to %s — memory step skipped", defaultMem, vaultMem),
			})
		} else {
			res.Steps = append(res.Steps, Step{
				Kind:        "mkdir",
				Description: fmt.Sprintf("ensure %s exists", vaultMem),
				Apply: func() error {
					return os.MkdirAll(vaultMem, 0o755)
				},
			})

			if dirExists(defaultMem) && !isEmptyDir(defaultMem) {
				res.Steps = append(res.Steps, Step{
					Kind:        "copy",
					Description: fmt.Sprintf("copy %s → %s", defaultMem, vaultMem),
					Apply: func() error {
						return copyTree(defaultMem, vaultMem)
					},
				})
			}

			if dirExists(defaultMem) {
				res.Steps = append(res.Steps, Step{
					Kind:        "rmdir",
					Description: fmt.Sprintf("remove %s (after migration)", defaultMem),
					Apply: func() error {
						return os.RemoveAll(defaultMem)
					},
				})
			}

			res.Steps = append(res.Steps, Step{
				Kind:        "symlink",
				Description: fmt.Sprintf("symlink %s → %s", defaultMem, vaultMem),
				Apply: func() error {
					if err := os.MkdirAll(filepath.Dir(defaultMem), 0o755); err != nil {
						return err
					}
					return os.Symlink(vaultMem, defaultMem)
				},
			})
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

func planUnbridgeOpenClaw(scope, vaultRoot string, opts Options) (*Result, error) {
	if scope == "" {
		return nil, fmt.Errorf("openclaw unbridge requires a workspace name")
	}
	home, err := homeDir()
	if err != nil {
		return nil, err
	}

	configPath := filepath.Join(home, openclawConfigRel)
	defaultMem := filepath.Join(home, ".openclaw", scope, "memory")
	vaultMem := vaultSubdir(vaultRoot, HarnessOpenClaw, scope)

	res := &Result{Harness: HarnessOpenClaw, Scope: scope, DryRun: opts.DryRun}

	if !opts.SkipMCP {
		res.Steps = append(res.Steps, Step{
			Kind:        "mcp-edit",
			Description: fmt.Sprintf("remove mcp.servers.%s entry from %s", mcpServerName, configPath),
			Apply: func() error {
				return clearOpenClawMCPServer(configPath, mcpServerName)
			},
		})
	}

	if !opts.SkipMemory {
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
			Apply: func() error {
				return os.Remove(defaultMem)
			},
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
				Apply: func() error {
					return os.RemoveAll(vaultMem)
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

// setOpenClawMCPServer adds mega-mem's MCP entry under mcp.servers in the
// OpenClaw config. The `transport: "http"` field matches the convention
// OpenClaw uses for HTTP/SSE-style MCP server URLs (as opposed to stdio
// command/args entries). Other servers in mcp.servers are preserved.
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

// writeJSONObjectPreservingFormat writes JSON with 2-space indent. OpenClaw
// configs tend to be hand-edited; we don't try to preserve every quirk of
// the original formatting, just produce valid JSON.
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
