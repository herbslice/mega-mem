// Package bridge wires harness-native memory directories into a mega-mem vault.
//
// Three harnesses are supported in v1:
//
//   - Claude Code: redirected via the `autoMemoryDirectory` setting in
//     ~/.claude/settings.json. Per-project memory dirs land under the vault
//     automatically once the redirect is in place.
//
//   - Codex: redirected via a filesystem symlink at ~/.codex/memories/, since
//     Codex has no equivalent settings knob.
//
//   - OpenClaw: redirected via the `agents.defaults.workspace` field in
//     ~/.openclaw/openclaw.json.
//
// All bridge operations are dry-run-by-default. Apply only writes to disk
// when DryRun is false.
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
	// KeepVault, on Unbridge, leaves the vault subtree intact after copying
	// content back to the harness's default location. Defaults to true to
	// avoid surprising data loss.
	KeepVault bool
	// SkipMemory disables the memory-redirect half of bridge/unbridge.
	// Useful when you only want to wire (or unwire) the MCP server and
	// already have memory bridged or want to manage it separately.
	SkipMemory bool
	// SkipMCP disables the MCP-server-wiring half of bridge/unbridge.
	// Useful when you don't run mega-mem's MCP server (e.g., you prefer
	// hook-only injection) or want to wire MCP later by hand.
	SkipMCP bool
	// MCPURL is the SSE endpoint for mega-mem's MCP server, written into
	// each harness's MCP config. Defaults to the engine's bind address
	// when empty (the caller is expected to fill this in).
	MCPURL string
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
}

// DefaultMCPURL is the SSE endpoint mega-mem's MCP server exposes when the
// engine config uses the default bind address. Bridge writes this into the
// harness's MCP config when Options.MCPURL is empty.
const DefaultMCPURL = "http://127.0.0.1:8111/sse"

// Bridge wires the harness's memory store into the vault under
// agent-memory/<harness>/<scope>/. The exact mechanism depends on the harness:
// see the package doc comment.
func Bridge(harness Harness, scope, vaultRoot string, opts Options) (*Result, error) {
	vaultRoot, err := filepath.Abs(vaultRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve vault root: %w", err)
	}
	if opts.MCPURL == "" {
		opts.MCPURL = DefaultMCPURL
	}
	switch harness {
	case HarnessClaudeCode:
		return planBridgeClaudeCode(scope, vaultRoot, opts)
	case HarnessCodex:
		return planBridgeCodex(scope, vaultRoot, opts)
	case HarnessOpenClaw:
		return planBridgeOpenClaw(scope, vaultRoot, opts)
	case HarnessHermes:
		return planBridgeHermes(scope, vaultRoot, opts)
	default:
		return nil, fmt.Errorf("unsupported harness %q", harness)
	}
}

// Unbridge reverses a Bridge: copies vault content back to the harness's
// default location and removes the redirect (config edit or symlink).
func Unbridge(harness Harness, scope, vaultRoot string, opts Options) (*Result, error) {
	vaultRoot, err := filepath.Abs(vaultRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve vault root: %w", err)
	}
	if opts.MCPURL == "" {
		opts.MCPURL = DefaultMCPURL
	}
	switch harness {
	case HarnessClaudeCode:
		return planUnbridgeClaudeCode(scope, vaultRoot, opts)
	case HarnessCodex:
		return planUnbridgeCodex(scope, vaultRoot, opts)
	case HarnessOpenClaw:
		return planUnbridgeOpenClaw(scope, vaultRoot, opts)
	case HarnessHermes:
		return planUnbridgeHermes(scope, vaultRoot, opts)
	default:
		return nil, fmt.Errorf("unsupported harness %q", harness)
	}
}

// vaultSubdir returns the vault path for a (harness, scope) pair:
// <vaultRoot>/agent-memory/<harness>/<scope>/.
func vaultSubdir(vaultRoot string, harness Harness, scope string) string {
	return filepath.Join(vaultRoot, "agent-memory", string(harness), scope)
}
