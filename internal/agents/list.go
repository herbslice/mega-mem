// Package agents discovers harness installations on the machine and
// reports their bridge state for `mm agents list`.
package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/herbslice/mega-mem/internal/bridge"
	"github.com/herbslice/mega-mem/internal/config"
)

// Status is one row in `mm agents list`.
type Status struct {
	Harness       string `json:"harness"`
	Installed     bool   `json:"installed"`
	MCPWired      bool   `json:"mcp_wired"`
	MemoryBridged bool   `json:"memory_bridged"`
	// Vault is the alias of the registered vault the bridge points into.
	// Empty when not bridged or when the symlink target doesn't match
	// any registered vault path.
	Vault string `json:"vault,omitempty"`
	// Scope is a human-readable summary of what's bridged. For Claude Code
	// it's a project count ("all (4 projects)"); for OpenClaw it's the
	// list of bridged workspaces; for Codex/Hermes it's the vault subdir
	// name.
	Scope        string `json:"scope,omitempty"`
	HooksEnabled bool   `json:"hooks_enabled"`
}

// List gathers status for every supported harness. Pass a registry so
// memory-bridge inference can match symlink targets against registered
// vault paths; pass state so hook flags come from the same source of truth.
func List(reg *config.Registry, state *config.State) ([]Status, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("locate home: %w", err)
	}
	var rows []Status
	for _, h := range bridge.SupportedHarnesses() {
		s := Status{
			Harness:      string(h),
			HooksEnabled: state.HooksEnabledForHarness(string(h)),
		}
		switch h {
		case bridge.HarnessClaudeCode:
			s.Installed = dirExists(filepath.Join(home, ".claude"))
			s.MCPWired = claudeMCPWired(home)
			s.MemoryBridged, s.Vault, s.Scope = claudeMemoryStatus(home, reg)
		case bridge.HarnessCodex:
			s.Installed = dirExists(filepath.Join(home, ".codex"))
			s.MCPWired = codexMCPWired(home)
			s.MemoryBridged, s.Vault, s.Scope = codexMemoryStatus(home, reg)
		case bridge.HarnessHermes:
			s.Installed = dirExists(filepath.Join(home, ".hermes"))
			s.MCPWired = hermesMCPWired(home)
			s.MemoryBridged, s.Vault, s.Scope = hermesMemoryStatus(home, reg)
		case bridge.HarnessOpenClaw:
			s.Installed = dirExists(filepath.Join(home, ".openclaw"))
			s.MCPWired = openclawMCPWired(home)
			s.MemoryBridged, s.Vault, s.Scope = openclawMemoryStatus(home, reg)
		}
		rows = append(rows, s)
	}
	return rows, nil
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// vaultAliasForPath returns the alias whose registered path equals or
// contains target. Used to attribute a symlink target to a known vault.
// Both sides are EvalSymlinks-resolved for bind-mount / macOS-/private/
// handling.
func vaultAliasForPath(reg *config.Registry, target string) string {
	if reg == nil {
		return ""
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	for alias, entry := range reg.Vaults {
		rp, err := filepath.Abs(entry.Path)
		if err != nil {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(rp); err == nil {
			rp = resolved
		}
		if abs == rp || strings.HasPrefix(abs, rp+string(filepath.Separator)) {
			return alias
		}
	}
	return ""
}

// readSymlink returns the absolute target of path; empty string on error.
func readSymlink(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return target
}

func claudeMCPWired(home string) bool {
	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		return false
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return false
	}
	servers, _ := settings["mcpServers"].(map[string]any)
	_, ok := servers["mega-mem"]
	return ok
}

func claudeMemoryStatus(home string, reg *config.Registry) (bridged bool, vault, scope string) {
	projects := filepath.Join(home, ".claude", "projects")
	target := readSymlink(projects)
	if target == "" {
		return false, "", ""
	}
	alias := vaultAliasForPath(reg, target)
	if alias == "" {
		return false, "", ""
	}
	n := countSubdirs(projects)
	return true, alias, fmt.Sprintf("all (%d project%s)", n, plural(n))
}

func codexMCPWired(home string) bool {
	data, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "[mcp_servers.mega-mem]")
}

func codexMemoryStatus(home string, reg *config.Registry) (bridged bool, vault, scope string) {
	target := readSymlink(filepath.Join(home, ".codex", "memories"))
	if target == "" {
		return false, "", ""
	}
	alias := vaultAliasForPath(reg, target)
	if alias == "" {
		return false, "", ""
	}
	return true, alias, filepath.Base(target)
}

func hermesMCPWired(home string) bool {
	data, err := os.ReadFile(filepath.Join(home, ".hermes", "config.yaml"))
	if err != nil {
		return false
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return false
	}
	servers, _ := cfg["mcp_servers"].(map[string]any)
	_, ok := servers["mega-mem"]
	return ok
}

func hermesMemoryStatus(home string, reg *config.Registry) (bridged bool, vault, scope string) {
	target := readSymlink(filepath.Join(home, ".hermes", "memories"))
	if target == "" {
		return false, "", ""
	}
	alias := vaultAliasForPath(reg, target)
	if alias == "" {
		return false, "", ""
	}
	return true, alias, filepath.Base(target)
}

func openclawMCPWired(home string) bool {
	data, err := os.ReadFile(filepath.Join(home, ".openclaw", "openclaw.json"))
	if err != nil {
		return false
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false
	}
	mcp, _ := cfg["mcp"].(map[string]any)
	servers, _ := mcp["servers"].(map[string]any)
	_, ok := servers["mega-mem"]
	return ok
}

func openclawMemoryStatus(home string, reg *config.Registry) (bridged bool, vault, scope string) {
	root := filepath.Join(home, ".openclaw")
	entries, err := os.ReadDir(root)
	if err != nil {
		return false, "", ""
	}
	var workspaces []string
	var anyAlias string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "memory" {
			continue
		}
		memPath := filepath.Join(root, e.Name(), "memory")
		target := readSymlink(memPath)
		if target == "" {
			continue
		}
		alias := vaultAliasForPath(reg, target)
		if alias == "" {
			continue
		}
		workspaces = append(workspaces, e.Name())
		anyAlias = alias
	}
	if len(workspaces) == 0 {
		return false, "", ""
	}
	sort.Strings(workspaces)
	return true, anyAlias, strings.Join(workspaces, ",")
}

func countSubdirs(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			n++
		}
	}
	return n
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
