// Package bridge wires harness-native memory directories into a mega-mem vault.
//
// Four harnesses are supported in v1:
//
//   - Claude Code: ~/.claude/projects/ symlinked into the vault when
//     IncludeMemory is set; without it, the bridge only adds mega-mem to
//     ~/.claude/settings.json's mcpServers map.
//
//   - Codex / Hermes: single-directory harnesses; ~/.codex/memories/ and
//     ~/.hermes/memories/ are symlink-replaced into the vault.
//
//   - OpenClaw: per-workspace symlink-replace at ~/.openclaw/<ws>/memory/.
//     With IncludeMemory and no Scope, every workspace is bridged; pass
//     Scope to filter to one workspace.
//
// All bridge operations are dry-run-by-default. Apply only writes to disk
// when DryRun is false. Memory bridging is opt-in (IncludeMemory=true) —
// by default Bridge only wires the MCP server.
package bridge

import (
	"fmt"
	"path/filepath"
)

// Harness identifies one of the supported integration targets.
type Harness string

const (
	HarnessClaudeCode Harness = "claude-code"
	HarnessCodex      Harness = "codex"
	HarnessOpenClaw   Harness = "openclaw"
	HarnessHermes     Harness = "hermes"
)

// SupportedHarnesses returns the canonical list, ordered.
func SupportedHarnesses() []Harness {
	return []Harness{HarnessClaudeCode, HarnessCodex, HarnessOpenClaw, HarnessHermes}
}

// ParseHarness converts a CLI-supplied name into a Harness, returning an
// error for unknown values. Names are matched case-insensitively against the
// canonical forms.
func ParseHarness(s string) (Harness, error) {
	for _, h := range SupportedHarnesses() {
		if string(h) == s {
			return h, nil
		}
	}
	return "", fmt.Errorf("unknown harness %q (known: claude-code, codex, openclaw, hermes)", s)
}

// Options controls bridge / unbridge execution.
type Options struct {
	// DryRun reports the steps that would run without writing anything.
	DryRun bool

	// Scope optionally narrows the bridge. Empty = harness-defined default
	// (Claude Code: whole projects/ dir; OpenClaw: all workspaces;
	// Codex/Hermes: single "memories" subdir). Non-empty = a specific
	// workspace/slug/named pool.
	Scope string

	// IncludeMemory enables the filesystem-moving memory bridge. Default
	// false: Bridge only wires the MCP server. When true, the per-harness
	// memory mechanism runs (symlink-replace in most cases).
	IncludeMemory bool

	// SkipMCP disables the MCP-config wiring. Default false. Combined with
	// !IncludeMemory makes Bridge a no-op.
	SkipMCP bool

	// KeepVault, on Unbridge, leaves the vault subtree intact after copying
	// content back to the harness's default location. Defaults to false:
	// unbridge cleans up its target.
	KeepVault bool

	// MCPURL is the SSE endpoint mega-mem's MCP server exposes. Defaults
	// to DefaultMCPURL when empty.
	MCPURL string

	// ListScopes, when set on Bridge(), short-circuits the planning: returns
	// a Result with no Apply steps but with Scopes populated, listing the
	// names a user could pass as Scope. Definition of "scope" depends on
	// the harness (CC: project slug; OpenClaw: workspace name; Codex/Hermes:
	// existing vault subdir name).
	ListScopes bool
}

// Step describes one filesystem or config mutation in a bridge plan.
type Step struct {
	// Kind is a short tag like "copy", "symlink", "settings-edit".
	Kind string
	// Description is a human-readable one-liner shown to the user.
	Description string
	// Apply, when invoked, performs the mutation. Nil for purely
	// informational steps.
	Apply func() error `json:"-"`
}

// Result is what Bridge / Unbridge return: the planned steps and, when not
// a dry run, an indication of which ones executed successfully.
type Result struct {
	Harness  Harness
	Scope    string
	DryRun   bool
	Steps    []Step
	Executed int

	// Scopes is populated by Bridge when ListScopes is true, listing
	// discoverable scope names for this harness.
	Scopes []string
}

// DefaultMCPURL is the SSE endpoint mega-mem's MCP server exposes when the
// engine config uses the default bind address. Bridge writes this into the
// harness's MCP config when Options.MCPURL is empty.
const DefaultMCPURL = "http://127.0.0.1:8111/sse"

// Bridge wires the harness's MCP server into its config and (when
// opts.IncludeMemory) redirects its memory directory into the vault.
func Bridge(harness Harness, vaultRoot string, opts Options) (*Result, error) {
	vaultRoot, err := filepath.Abs(vaultRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve vault root: %w", err)
	}
	if opts.MCPURL == "" {
		opts.MCPURL = DefaultMCPURL
	}
	if opts.ListScopes {
		return planListScopes(harness, vaultRoot)
	}
	switch harness {
	case HarnessClaudeCode:
		return planBridgeClaudeCode(vaultRoot, opts)
	case HarnessCodex:
		return planBridgeCodex(vaultRoot, opts)
	case HarnessOpenClaw:
		return planBridgeOpenClaw(vaultRoot, opts)
	case HarnessHermes:
		return planBridgeHermes(vaultRoot, opts)
	default:
		return nil, fmt.Errorf("unsupported harness %q", harness)
	}
}

// Unbridge reverses a Bridge: removes the MCP entry and (when
// opts.IncludeMemory) the memory redirect, copying content back to the
// harness's default location.
func Unbridge(harness Harness, vaultRoot string, opts Options) (*Result, error) {
	vaultRoot, err := filepath.Abs(vaultRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve vault root: %w", err)
	}
	if opts.MCPURL == "" {
		opts.MCPURL = DefaultMCPURL
	}
	switch harness {
	case HarnessClaudeCode:
		return planUnbridgeClaudeCode(vaultRoot, opts)
	case HarnessCodex:
		return planUnbridgeCodex(vaultRoot, opts)
	case HarnessOpenClaw:
		return planUnbridgeOpenClaw(vaultRoot, opts)
	case HarnessHermes:
		return planUnbridgeHermes(vaultRoot, opts)
	default:
		return nil, fmt.Errorf("unsupported harness %q", harness)
	}
}

// planListScopes dispatches to the per-harness scope enumerator.
func planListScopes(harness Harness, vaultRoot string) (*Result, error) {
	res := &Result{Harness: harness, DryRun: true}
	var scopes []string
	var err error
	switch harness {
	case HarnessClaudeCode:
		scopes, err = listClaudeScopes()
	case HarnessCodex:
		scopes, err = listVaultScopes(vaultRoot, HarnessCodex)
	case HarnessHermes:
		scopes, err = listVaultScopes(vaultRoot, HarnessHermes)
	case HarnessOpenClaw:
		scopes, err = listOpenClawScopes()
	default:
		return nil, fmt.Errorf("unsupported harness %q", harness)
	}
	if err != nil {
		return nil, err
	}
	res.Scopes = scopes
	return res, nil
}

// applySteps runs each step's Apply unless DryRun. Stops on first error
// and returns the executed count so far.
func applySteps(res *Result, dryRun bool) error {
	if dryRun {
		return nil
	}
	for _, st := range res.Steps {
		if st.Apply == nil {
			continue
		}
		if err := st.Apply(); err != nil {
			return fmt.Errorf("%s: %w", st.Description, err)
		}
		res.Executed++
	}
	return nil
}

// listVaultScopes returns the directory names directly under
// <vaultRoot>/agent-memory/<harness>/. Used for the Codex / Hermes
// --list-scopes path: those harnesses have user-named pools and there's no
// other discovery channel beyond inspecting what already exists in the
// vault.
func listVaultScopes(vaultRoot string, harness Harness) ([]string, error) {
	root := filepath.Join(vaultRoot, "agent-memory", string(harness))
	if !dirExists(root) {
		return nil, nil
	}
	return listDirNames(root)
}
