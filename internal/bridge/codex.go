package bridge

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Codex memory layout:
//
//   ~/.codex/memories/<topic>.md
//
// Codex has no `memoryDirectory` setting equivalent (per the CLI source
// inspection done while drafting this), so the only redirect mechanism is a
// filesystem symlink at ~/.codex/memories/. Bridge migrates any existing
// files into the vault and replaces the original directory with a symlink
// pointing at the vault subtree. Memory bridging is opt-in via
// Options.IncludeMemory; by default Bridge only wires the MCP server.

const (
	codexMemoriesRel = ".codex/memories"
	codexConfigRel   = ".codex/config.toml"
	codexDefaultPool = "memories"
)

// codexVaultMem returns the vault subdir for a given scope.
//
//	scope == ""   → <vault>/agent-memory/codex/memories/  (default pool)
//	scope == "x"  → <vault>/agent-memory/codex/x/         (named pool)
func codexVaultMem(vaultRoot, scope string) string {
	if scope == "" {
		scope = codexDefaultPool
	}
	return filepath.Join(vaultRoot, "agent-memory", "codex", scope)
}

func planBridgeCodex(vaultRoot string, opts Options) (*Result, error) {
	home, err := homeDir()
	if err != nil {
		return nil, err
	}

	defaultMem := filepath.Join(home, codexMemoriesRel)
	configPath := filepath.Join(home, codexConfigRel)
	vaultMem := codexVaultMem(vaultRoot, opts.Scope)

	res := &Result{Harness: HarnessCodex, Scope: opts.Scope, DryRun: opts.DryRun}

	if opts.IncludeMemory {
		steps, err := codexMemorySteps(defaultMem, vaultMem)
		if err != nil {
			return nil, err
		}
		res.Steps = append(res.Steps, steps...)
	}

	if !opts.SkipMCP {
		res.Steps = append(res.Steps, Step{
			Kind:        "mcp-edit",
			Description: fmt.Sprintf("add [mcp_servers.%s] (url=%s) to %s", mcpServerName, opts.MCPURL, configPath),
			Apply: func() error {
				return setCodexMCPServer(configPath, mcpServerName, opts.MCPURL)
			},
		})
	}

	if err := applySteps(res, opts.DryRun); err != nil {
		return res, err
	}
	return res, nil
}

// codexMemorySteps builds the symlink-replace plan for Codex's single
// memories/ directory. Idempotent: if defaultMem is already a symlink to
// vaultMem, returns a single noop step.
func codexMemorySteps(defaultMem, vaultMem string) ([]Step, error) {
	var steps []Step
	if isSymlink(defaultMem) {
		target, err := os.Readlink(defaultMem)
		if err != nil {
			return nil, fmt.Errorf("read existing symlink %s: %w", defaultMem, err)
		}
		if target != vaultMem {
			return nil, fmt.Errorf("%s already symlinked to %s; remove it before bridging", defaultMem, target)
		}
		steps = append(steps, Step{
			Kind:        "noop",
			Description: fmt.Sprintf("%s already symlinked to %s — memory step skipped", defaultMem, vaultMem),
		})
		return steps, nil
	}

	steps = append(steps, Step{
		Kind:        "mkdir",
		Description: fmt.Sprintf("ensure %s exists", vaultMem),
		Apply:       func() error { return os.MkdirAll(vaultMem, 0o755) },
	})

	if dirExists(defaultMem) && !isEmptyDir(defaultMem) {
		steps = append(steps, Step{
			Kind:        "copy",
			Description: fmt.Sprintf("copy %s → %s", defaultMem, vaultMem),
			Apply:       func() error { return copyTree(defaultMem, vaultMem) },
		})
	}
	if dirExists(defaultMem) {
		steps = append(steps, Step{
			Kind:        "rmdir",
			Description: fmt.Sprintf("remove %s (after migration)", defaultMem),
			Apply:       func() error { return os.RemoveAll(defaultMem) },
		})
	}
	steps = append(steps, Step{
		Kind:        "symlink",
		Description: fmt.Sprintf("symlink %s → %s", defaultMem, vaultMem),
		Apply: func() error {
			if err := os.MkdirAll(filepath.Dir(defaultMem), 0o755); err != nil {
				return err
			}
			return os.Symlink(vaultMem, defaultMem)
		},
	})
	return steps, nil
}

func planUnbridgeCodex(vaultRoot string, opts Options) (*Result, error) {
	home, err := homeDir()
	if err != nil {
		return nil, err
	}

	defaultMem := filepath.Join(home, codexMemoriesRel)
	configPath := filepath.Join(home, codexConfigRel)
	vaultMem := codexVaultMem(vaultRoot, opts.Scope)

	res := &Result{Harness: HarnessCodex, Scope: opts.Scope, DryRun: opts.DryRun}

	if !opts.SkipMCP {
		res.Steps = append(res.Steps, Step{
			Kind:        "mcp-edit",
			Description: fmt.Sprintf("remove [mcp_servers.%s] section from %s", mcpServerName, configPath),
			Apply: func() error {
				return clearCodexMCPServer(configPath, mcpServerName)
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

// setCodexMCPServer appends or updates the [mcp_servers.<name>] section in
// Codex's config.toml. If the section already exists with the same URL,
// no-op. If it exists with a different URL, replaces it. Other content in
// config.toml is preserved (line-oriented edit; comments above the section
// stay in place).
func setCodexMCPServer(configPath, name, url string) error {
	header := fmt.Sprintf("[mcp_servers.%s]", name)
	body := fmt.Sprintf("url = %q\n", url)

	existing, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		existing = nil
	}

	rest, hadSection := excludeTomlSection(string(existing), header)
	if hadSection && strings.Contains(string(existing), body) {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}

	var sb strings.Builder
	sb.WriteString(rest)
	if !strings.HasSuffix(rest, "\n") && rest != "" {
		sb.WriteString("\n")
	}
	if rest != "" {
		sb.WriteString("\n")
	}
	sb.WriteString(header)
	sb.WriteString("\n")
	sb.WriteString(body)
	return os.WriteFile(configPath, []byte(sb.String()), 0o644)
}

// clearCodexMCPServer removes the [mcp_servers.<name>] section from Codex's
// config.toml. Missing file or absent section: no-op. Other sections are
// preserved.
func clearCodexMCPServer(configPath, name string) error {
	header := fmt.Sprintf("[mcp_servers.%s]", name)
	existing, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	rest, _ := excludeTomlSection(string(existing), header)
	return os.WriteFile(configPath, []byte(rest), 0o644)
}

// excludeTomlSection removes the named section from a TOML document by
// finding the matching [section.header] line and discarding everything up
// to (but not including) the next section header (`[...]` line) or EOF.
// Returns the remaining content and a flag for whether the section existed.
// Best-effort: doesn't honor inline tables or arrays-of-tables.
func excludeTomlSection(input, header string) (string, bool) {
	if input == "" {
		return "", false
	}
	scanner := bufio.NewScanner(strings.NewReader(input))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out strings.Builder
	skipping := false
	found := false
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		isHeader := strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")
		switch {
		case skipping && isHeader:
			skipping = false
			out.WriteString(line)
			out.WriteByte('\n')
		case skipping:
			// drop this line
		case trimmed == header:
			skipping = true
			found = true
		default:
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	// Best-effort error swallow on read; we already have what we processed.
	_ = scanner.Err()
	return out.String(), found
}
