package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/herbslice/mega-mem/internal/bridge"
	"github.com/herbslice/mega-mem/internal/config"
	"github.com/herbslice/mega-mem/internal/scaffold"
	"github.com/herbslice/mega-mem/internal/server"
	"github.com/herbslice/mega-mem/internal/templates"
	"github.com/herbslice/mega-mem/internal/vault"
)

// version is overridden at build time via -ldflags. Default is "0.0.0"
// to denote pre-release alpha (no public release tagged yet).
var version = "0.0.0"

// vaultRef is set by extractVaultRef() before cobra runs. Subcommands of the
// "vault" tree read it to know which vault they're operating on.
var vaultRef string

// templatesDirFlag is a persistent root flag that prepends a directory to the
// template search path. Takes highest priority.
var templatesDirFlag string

// mkResolver builds a templates.Resolver honoring --templates-dir plus any
// additional per-call extras (e.g., the current vault's override dir).
func mkResolver(extras ...string) *templates.Resolver {
	all := append([]string(nil), extras...)
	if templatesDirFlag != "" {
		all = append([]string{templatesDirFlag}, all...)
	}
	return templates.NewResolver(all...)
}

func main() {
	preprocess()
	if err := newRootCmd().Execute(); err != nil {
		var skipped *scaffold.SkippedError
		if errors.As(err, &skipped) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// vaultSubcommands are the valid verbs under the "vault" subcommand. Used by
// preprocess to tell apart `vault <ref> <verb>` (extract ref) from
// `vault <verb>` missing ref (don't extract) and `vault <verb> --help`
// (also don't extract — let cobra show the subcommand's help).
var vaultSubcommands = map[string]bool{
	"init":     true,
	"scaffold": true,
	"serve":    true,
	"status":   true,
	"bridge":   true,
	"unbridge": true,
}

// preprocess extracts the vault ref from `mega-mem [...] vault <ref> <verb>`
// and mutates os.Args so cobra sees `mega-mem [...] vault <verb>`. Flags are
// left intact for cobra to parse normally. Only extracts when the token after
// the candidate ref is a known subcommand, which disambiguates the
// "vault <verb> [--help]" case (no ref) from "vault <ref> <verb>" (ref).
func preprocess() {
	args := os.Args
	for j := 1; j < len(args); j++ {
		if args[j] != "vault" {
			continue
		}
		if j+2 >= len(args) {
			return
		}
		if strings.HasPrefix(args[j+1], "-") {
			return
		}
		if !vaultSubcommands[args[j+2]] {
			return
		}
		vaultRef = args[j+1]
		os.Args = append(append([]string(nil), args[:j+1]...), args[j+2:]...)
		return
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "mega-mem",
		Short:   "Agent-agnostic personal knowledge base + TODO system served over MCP",
		Version: version,
		// Suppress cobra's default error/usage printout on RunE failure —
		// main() is authoritative for error output and exit code mapping.
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&templatesDirFlag, "templates-dir", "",
		"Prepend this directory to the template search path (highest priority)")
	root.AddCommand(newVaultCmd(), newVaultsCmd(), newTemplateCmd(), newHooksCmd())
	return root
}

// --- hooks (machine-local toggle) ---

func newHooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Toggle mega-mem hook injection (machine-local)",
		Long: `Toggle whether mega-mem hook scripts inject context into your harness.

State lives at ~/.config/mega-mem/state.yaml (machine-local; not synced).
The shipped hook scripts read this file at the top of each invocation, so
toggling takes effect on the next prompt without restarting the harness.

This is independent of per-request "persona" tuning passed via the recall
MCP tool — use the toggle when you want hooks off entirely for a stretch
of work; use a persona when you want different recall settings per call.`,
	}
	cmd.AddCommand(newHooksEnableCmd(), newHooksDisableCmd(), newHooksStatusCmd())
	return cmd
}

func newHooksEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Re-enable mega-mem hook injection",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := config.SetHooksEnabled(true); err != nil {
				return err
			}
			fmt.Println("hooks: enabled")
			return nil
		},
	}
}

func newHooksDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable mega-mem hook injection without unwiring it",
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := config.SetHooksEnabled(false); err != nil {
				return err
			}
			fmt.Println("hooks: disabled")
			return nil
		},
	}
}

func newHooksStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether hooks are currently enabled",
		RunE: func(_ *cobra.Command, _ []string) error {
			s, err := config.LoadState()
			if err != nil {
				return err
			}
			path, _ := config.StatePath()
			label := "enabled"
			if !s.HooksEnabledOrDefault() {
				label = "disabled"
			}
			fmt.Printf("hooks: %s\n", label)
			fmt.Printf("state: %s\n", path)
			return nil
		},
	}
}

// --- vault <ref> <subcommand> ---

func newVaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault <alias> <subcommand>",
		Short: "Operate on a specific vault by alias",
		Long: `Operate on a specific vault identified by its alias.

All vault operations take the form:

    mega-mem vault <alias> <subcommand> [flags]

<alias> is a vault registered via 'mega-mem vaults register'.
See 'mega-mem vaults list' for registered aliases.`,
	}
	cmd.AddCommand(newInitCmd(), newScaffoldCmd(), newServeCmd(), newStatusCmd(), newBridgeCmd(), newUnbridgeCmd())
	return cmd
}

func requireVaultRef() (string, error) {
	if vaultRef == "" {
		return "", fmt.Errorf("vault reference required: use `mega-mem vault <ref> <subcommand>`")
	}
	return config.ResolveRef(vaultRef)
}

func newInitCmd() *cobra.Command {
	var force, dryRun, gitInit bool
	var rootTemplate string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a new vault from the root template",
		Long: `Scaffold the registered vault using the root template (default: vault-root).

Invoke as:  mega-mem vault <alias> init [flags]

Safe on non-empty directories: folders that exist are no-ops; files with
matching content are no-ops; differing files are skipped unless --force.
Leaves an existing .mega-mem.yaml untouched unless --force.

The --git flag runs 'git init' (if .git is missing) and writes a starter
.gitignore that excludes per-machine search index, sync-conflict files,
and editor backups. mega-mem does not wrap commit/push — see
docs/SYNC-SUGGESTIONS.md for cron and pre-commit recipes.`,
		Example: `  mega-mem vault mykb init
  mega-mem vault mykb init --dry-run
  mega-mem vault mykb init --git
  mega-mem vault mykb init --force`,
		RunE: func(_ *cobra.Command, _ []string) error {
			vp, err := requireVaultRef()
			if err != nil {
				return err
			}
			return vault.Init(vp, vault.InitOpts{
				Force:        force,
				DryRun:       dryRun,
				RootTemplate: rootTemplate,
				TemplatesDir: templatesDirFlag,
				Git:          gitInit,
			})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Use an existing non-empty directory and overwrite conflicting files")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would happen without writing")
	cmd.Flags().StringVar(&rootTemplate, "root-template", "", "Override the root template name (default: vault-root)")
	cmd.Flags().BoolVar(&gitInit, "git", false, "Run 'git init' and write a starter .gitignore")
	return cmd
}

func newScaffoldCmd() *cobra.Command {
	var force, dryRun, diff, noRecurse, tree bool
	var format string
	cmd := &cobra.Command{
		Use:   "scaffold [<template> [<subpath>]]",
		Short: "Apply a template to a vault subpath (or reconcile the whole vault if no args)",
		Long: `Apply a named template to a subpath of the vault.

Argument forms:
    scaffold                    # apply 'vault-root' at vault root (full reconcile)
    scaffold <template>         # apply <template> at vault root
    scaffold <template> <path>  # apply <template> at <path> inside the vault

Children: declarations recurse into subdirectories depth-first.`,
		Example: `  mega-mem vault mykb scaffold                       # reconcile whole vault
  mega-mem vault mykb scaffold org orgs/example      # add an org
  mega-mem vault mykb scaffold my-template           # apply at vault root
  mega-mem vault mykb scaffold --dry-run
  mega-mem vault mykb scaffold --diff --tree`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			vp, err := requireVaultRef()
			if err != nil {
				return err
			}
			var tplName, subpath string
			switch len(args) {
			case 0:
				tplName = "vault-root"
			case 1:
				tplName = args[0]
			case 2:
				tplName = args[0]
				subpath = args[1]
			}
			target := vp
			if subpath != "" {
				target = filepath.Join(vp, subpath)
			}
			res := mkResolver(templates.VaultOverridesDir(vp))
			tpl, err := res.Resolve(tplName)
			if err != nil {
				return err
			}
			plan, err := scaffold.Compute(res, tpl, target, scaffold.Options{
				Force:     force,
				Diff:      diff,
				NoRecurse: noRecurse,
			})
			if err != nil {
				return err
			}
			if dryRun || diff {
				return scaffold.Format(os.Stdout, plan, format, tree)
			}
			return scaffold.Apply(plan)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite conflicting files")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would happen without writing")
	cmd.Flags().BoolVar(&diff, "diff", false, "Like --dry-run plus extras (target contents not in template)")
	cmd.Flags().BoolVar(&noRecurse, "no-recurse", false, "Skip children: declarations")
	cmd.Flags().BoolVar(&tree, "tree", false, "Render dry-run/diff output as a tree")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	return cmd
}

func newServeCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:     "serve",
		Short:   "Run the MCP server for this vault",
		Example: `  mega-mem vault mykb serve`,
		RunE: func(_ *cobra.Command, _ []string) error {
			if vaultRef == "" {
				return fmt.Errorf("vault reference required: use `mega-mem vault <ref> serve`")
			}
			if configPath == "" {
				p, err := config.EngineConfigPathForAlias(vaultRef)
				if err != nil {
					return err
				}
				configPath = p
			}
			engineCfg, err := config.LoadEngine(configPath)
			if err != nil {
				return fmt.Errorf("load engine config: %w", err)
			}
			vaultCfg, err := config.LoadVault(engineCfg.VaultPath)
			if err != nil {
				return fmt.Errorf("load vault config: %w", err)
			}
			srv, err := server.New(engineCfg, vaultCfg)
			if err != nil {
				return fmt.Errorf("build server: %w", err)
			}
			return srv.Run(context.Background())
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to engine config YAML (default: ~/.config/mega-mem/engines/<alias>.yaml)")
	return cmd
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Short:   "Show basic information about the vault",
		Example: `  mega-mem vault mykb status`,
		RunE: func(_ *cobra.Command, _ []string) error {
			vp, err := requireVaultRef()
			if err != nil {
				return err
			}
			vc, err := config.LoadVault(vp)
			if err != nil {
				return err
			}
			fmt.Printf("vault_id: %s\n", vc.VaultID)
			fmt.Printf("path:     %s\n", vp)
			return nil
		},
	}
}

func newBridgeCmd() *cobra.Command {
	var apply, skipMemory, skipMCP bool
	var mcpURL string
	cmd := &cobra.Command{
		Use:   "bridge <harness> <scope>",
		Short: "Wire a harness's memory + MCP server into the vault",
		Long: `Redirect a harness's auto-memory location to live under
agent-memory/<harness>/<scope>/ in the vault, and add mega-mem's MCP server
to the harness's MCP-client config.

Per-harness memory mechanism:
  - claude-code:  edits autoMemoryDirectory in ~/.claude/settings.json
  - codex:        symlinks ~/.codex/memories/ to the vault subtree
  - openclaw:     edits agents.defaults.workspace in ~/.openclaw/openclaw.json
  - hermes:       symlinks ~/.hermes/memories/ to the vault subtree

Per-harness MCP-config edit:
  - claude-code:  adds mcpServers["mega-mem"] in ~/.claude/settings.json
  - codex:        adds [mcp_servers.mega-mem] in ~/.codex/config.toml
  - openclaw:     adds mcp.servers["mega-mem"] in ~/.openclaw/openclaw.json
  - hermes:       adds mcp_servers["mega-mem"] in ~/.hermes/config.yaml

Migrates any existing memory at the harness's default location into the
vault before applying the redirect.

Dry-run by default: prints the steps without modifying anything. Pass
--apply (before the positional args) to commit changes.

Note: Claude Code project slugs start with a dash (encoded path of the
project's git root). Put --apply BEFORE the positional args, or use --
to separate them.`,
		Example: `  mega-mem vault mykb bridge claude-code -home-user-work-myrepo
  mega-mem vault mykb bridge --apply claude-code -home-user-work-myrepo
  mega-mem vault mykb bridge codex personal --apply
  mega-mem vault mykb bridge openclaw workspace-main --apply
  mega-mem vault mykb bridge hermes shared --apply
  mega-mem vault mykb bridge --apply --no-mcp claude-code -home-...    # memory only
  mega-mem vault mykb bridge --apply --no-memory hermes shared        # MCP only`,
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			vp, err := requireVaultRef()
			if err != nil {
				return err
			}
			h, err := bridge.ParseHarness(args[0])
			if err != nil {
				return err
			}
			res, err := bridge.Bridge(h, args[1], vp, bridge.Options{
				DryRun:     !apply,
				SkipMemory: skipMemory,
				SkipMCP:    skipMCP,
				MCPURL:     mcpURL,
			})
			if err != nil {
				return err
			}
			printBridgeResult(os.Stdout, res, apply, "bridge")
			return nil
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "Commit changes (default: dry-run)")
	cmd.Flags().BoolVar(&skipMemory, "no-memory", false, "Skip the memory-redirect step (only edit MCP config)")
	cmd.Flags().BoolVar(&skipMCP, "no-mcp", false, "Skip the MCP-config step (only redirect memory)")
	cmd.Flags().StringVar(&mcpURL, "mcp-url", "", "Override the MCP server URL (default: http://127.0.0.1:8111/sse)")
	cmd.Flags().SetInterspersed(false)
	return cmd
}

func newUnbridgeCmd() *cobra.Command {
	var apply, keepVault, skipMemory, skipMCP bool
	cmd := &cobra.Command{
		Use:   "unbridge <harness> <scope>",
		Short: "Reverse a bridge: copy vault content back, remove the redirect",
		Long: `Undo a bridge by copying vault content back to the harness's default
location, removing the config redirect or symlink, and removing mega-mem
from the harness's MCP-client config.

By default, the vault subtree under agent-memory/<harness>/<scope>/ is
removed after the copy succeeds. Pass --keep-vault to preserve it.

Dry-run by default: prints the steps without modifying anything. Pass
--apply (before the positional args) to commit changes.`,
		Example: `  mega-mem vault mykb unbridge claude-code -home-user-work-myrepo
  mega-mem vault mykb unbridge --apply --keep-vault codex personal
  mega-mem vault mykb unbridge --apply --no-mcp hermes shared        # leave MCP wired`,
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			vp, err := requireVaultRef()
			if err != nil {
				return err
			}
			h, err := bridge.ParseHarness(args[0])
			if err != nil {
				return err
			}
			res, err := bridge.Unbridge(h, args[1], vp, bridge.Options{
				DryRun:     !apply,
				KeepVault:  keepVault,
				SkipMemory: skipMemory,
				SkipMCP:    skipMCP,
			})
			if err != nil {
				return err
			}
			printBridgeResult(os.Stdout, res, apply, "unbridge")
			return nil
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "Commit changes (default: dry-run)")
	cmd.Flags().BoolVar(&keepVault, "keep-vault", false, "Leave the vault subtree intact after unbridging")
	cmd.Flags().BoolVar(&skipMemory, "no-memory", false, "Skip the memory-restore step (only remove MCP config)")
	cmd.Flags().BoolVar(&skipMCP, "no-mcp", false, "Skip the MCP-config step (only restore memory)")
	cmd.Flags().SetInterspersed(false)
	return cmd
}

func printBridgeResult(w io.Writer, res *bridge.Result, applied bool, verb string) {
	header := fmt.Sprintf("%s %s/%s", verb, res.Harness, res.Scope)
	if !applied {
		header += " (dry-run; pass --apply to commit)"
	}
	fmt.Fprintln(w, header)
	for i, step := range res.Steps {
		marker := " "
		if applied && i < res.Executed {
			marker = "✓"
		}
		fmt.Fprintf(w, "  %s [%s] %s\n", marker, step.Kind, step.Description)
	}
}

// --- vaults (registry management) ---

func newVaultsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vaults",
		Short: "Manage the vault alias registry",
	}
	cmd.AddCommand(
		newVaultsListCmd(),
		newVaultsRegisterCmd(),
		newVaultsUnregisterCmd(),
		newVaultsRenameCmd(),
		newVaultsShowCmd(),
		newVaultsCheckCmd(),
	)
	return cmd
}

func newVaultsCheckCmd() *cobra.Command {
	var drift, conflicts bool
	var format string
	cmd := &cobra.Command{
		Use:   "check [<alias>]",
		Short: "Verify registered vault aliases point to valid directories",
		Long: `Check registered vault aliases against the filesystem.

Without an argument, checks every alias in the registry. With an alias,
checks only that one.

Statuses:
    OK            path exists, is a directory, has .mega-mem.yaml
    MISSING       registered path does not exist
    NOT_A_DIR     path exists but is not a directory
    NOT_A_VAULT   directory exists but has no .mega-mem.yaml
    DRIFT         (with --drift) vault-root template has missing/extra items
    CONFLICTS     (with --conflicts) Syncthing/Nextcloud sync-conflict files
                  or git merge artifacts found in the vault

Exit code 3 if any vault has an issue; 0 if all OK.`,
		Example: `  mega-mem vaults check
  mega-mem vaults check mykb
  mega-mem vaults check --drift
  mega-mem vaults check --conflicts
  mega-mem vaults check --format json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			reg, err := config.LoadRegistry()
			if err != nil {
				return err
			}
			var aliases []string
			if len(args) == 1 {
				if _, ok := reg.Vaults[args[0]]; !ok {
					return fmt.Errorf("alias %q not registered", args[0])
				}
				aliases = []string{args[0]}
			} else {
				for a := range reg.Vaults {
					aliases = append(aliases, a)
				}
				sort.Strings(aliases)
			}
			results := make([]checkResult, 0, len(aliases))
			for _, a := range aliases {
				results = append(results, checkVault(a, reg.Vaults[a].Path, drift, conflicts))
			}
			if err := printCheckResults(os.Stdout, results, format); err != nil {
				return err
			}
			issues := 0
			for _, r := range results {
				if r.Status != "ok" {
					issues++
				}
			}
			if issues > 0 {
				return &scaffold.SkippedError{N: issues}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&drift, "drift", false, "Also check for template drift (missing/extra items per vault-root)")
	cmd.Flags().BoolVar(&conflicts, "conflicts", false, "Also scan for Syncthing/Nextcloud sync-conflict files and git merge artifacts")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text | json")
	return cmd
}

type checkResult struct {
	Alias     string   `json:"alias"`
	Path      string   `json:"path"`
	Status    string   `json:"status"`
	Reason    string   `json:"reason,omitempty"`
	Missing   int      `json:"missing,omitempty"`
	Extras    int      `json:"extras,omitempty"`
	Conflicts []string `json:"conflicts,omitempty"`
}

func checkVault(alias, path string, drift, conflicts bool) checkResult {
	r := checkResult{Alias: alias, Path: path}

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.Status, r.Reason = "missing", "path does not exist"
		} else {
			r.Status, r.Reason = "missing", err.Error()
		}
		return r
	}
	if !info.IsDir() {
		r.Status, r.Reason = "not_a_dir", "path is not a directory"
		return r
	}
	if _, err := os.Stat(filepath.Join(path, ".mega-mem.yaml")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.Status, r.Reason = "not_a_vault", "no .mega-mem.yaml in directory"
		} else {
			r.Status, r.Reason = "not_a_vault", err.Error()
		}
		return r
	}

	if drift {
		res := mkResolver(templates.VaultOverridesDir(path))
		tpl, err := res.Resolve("vault-root")
		if err != nil {
			r.Status, r.Reason = "drift", "cannot resolve vault-root template: "+err.Error()
			return r
		}
		plan, err := scaffold.Compute(res, tpl, path, scaffold.Options{Diff: true})
		if err != nil {
			r.Status, r.Reason = "drift", err.Error()
			return r
		}
		for _, it := range plan.Items {
			switch it.Action {
			case scaffold.ActionCreate:
				r.Missing++
			case scaffold.ActionExtra:
				r.Extras++
			}
		}
		if r.Missing > 0 || r.Extras > 0 {
			r.Status = "drift"
			return r
		}
	}

	if conflicts {
		found, err := scanSyncConflicts(path)
		if err != nil {
			r.Status, r.Reason = "conflicts", "scan failed: "+err.Error()
			return r
		}
		if len(found) > 0 {
			r.Status = "conflicts"
			r.Conflicts = found
			return r
		}
	}

	r.Status = "ok"
	return r
}

// scanSyncConflicts walks the vault and returns paths matching common
// sync-conflict patterns: Syncthing's `.sync-conflict-*` infix, Nextcloud's
// `.~lock.*` markers, Dropbox's `*conflicted copy*` filenames, git merge
// artifacts (`*.orig`). Returned paths are relative to the vault root for
// compact output.
func scanSyncConflicts(root string) ([]string, error) {
	var hits []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip .git internals and the in-vault overrides/index dir.
			if name := info.Name(); name == ".git" || name == ".mega-mem" {
				return filepath.SkipDir
			}
			return nil
		}
		name := info.Name()
		switch {
		case strings.Contains(name, ".sync-conflict-"):
		case strings.HasPrefix(name, ".~lock."):
		case strings.Contains(strings.ToLower(name), "conflicted copy"):
		case strings.HasSuffix(name, ".orig"):
		default:
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			rel = p
		}
		hits = append(hits, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

func printCheckResults(w io.Writer, results []checkResult, format string) error {
	switch strings.ToLower(format) {
	case "json":
		out := struct {
			Results []checkResult `json:"results"`
			Summary struct {
				Total  int `json:"total"`
				OK     int `json:"ok"`
				Issues int `json:"issues"`
			} `json:"summary"`
		}{Results: results}
		for _, r := range results {
			out.Summary.Total++
			if r.Status == "ok" {
				out.Summary.OK++
			} else {
				out.Summary.Issues++
			}
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	case "", "text":
		maxAlias := 5
		maxStatus := 6
		for _, r := range results {
			if len(r.Alias) > maxAlias {
				maxAlias = len(r.Alias)
			}
			if s := strings.ToUpper(r.Status); len(s) > maxStatus {
				maxStatus = len(s)
			}
		}
		ok, issues := 0, 0
		for _, r := range results {
			suffix := r.Path
			switch {
			case r.Status == "drift" && r.Reason == "":
				suffix = fmt.Sprintf("%s (%d missing, %d extras)", r.Path, r.Missing, r.Extras)
			case r.Status == "conflicts" && r.Reason == "":
				suffix = fmt.Sprintf("%s (%d sync-conflict file(s))", r.Path, len(r.Conflicts))
			case r.Reason != "":
				suffix = fmt.Sprintf("%s (%s)", r.Path, r.Reason)
			}
			fmt.Fprintf(w, "%-*s  %-*s  %s\n", maxAlias, r.Alias, maxStatus, strings.ToUpper(r.Status), suffix)
			if r.Status == "conflicts" {
				for _, c := range r.Conflicts {
					fmt.Fprintf(w, "%-*s    %s\n", maxAlias+maxStatus+2, "", c)
				}
			}
			if r.Status == "ok" {
				ok++
			} else {
				issues++
			}
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "summary: %d OK, %d with issues\n", ok, issues)
		return nil
	default:
		return fmt.Errorf("unknown format %q (valid: text, json)", format)
	}
}

func newVaultsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List registered vault aliases",
		RunE: func(_ *cobra.Command, _ []string) error {
			reg, err := config.LoadRegistry()
			if err != nil {
				return err
			}
			if len(reg.Vaults) == 0 {
				fmt.Println("(no vaults registered)")
				return nil
			}
			names := make([]string, 0, len(reg.Vaults))
			for n := range reg.Vaults {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				fmt.Printf("%-16s  %s\n", n, reg.Vaults[n].Path)
			}
			return nil
		},
	}
}

func newVaultsRegisterCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "register <alias> [<path>]",
		Short: "Register a vault alias; if <path> is omitted, defaults to ~/.local/share/mega-mem/vaults/<alias>/",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			alias := args[0]
			if strings.ContainsAny(alias, "/\\.") {
				return fmt.Errorf("invalid alias %q: no slashes or dots", alias)
			}
			var path string
			if len(args) == 2 {
				abs, err := filepath.Abs(args[1])
				if err != nil {
					return err
				}
				path = abs
			} else {
				def, err := config.DefaultVaultPath(alias)
				if err != nil {
					return err
				}
				path = def
			}
			reg, err := config.LoadRegistry()
			if err != nil {
				return err
			}
			if _, exists := reg.Vaults[alias]; exists && !force {
				return fmt.Errorf("alias %q already registered (use --force to replace)", alias)
			}
			if err := os.MkdirAll(path, 0o755); err != nil {
				return fmt.Errorf("create vault dir %s: %w", path, err)
			}
			reg.Vaults[alias] = config.RegistryEntry{Path: path}
			if err := config.WriteRegistry(reg); err != nil {
				return err
			}
			fmt.Printf("registered: %s → %s\n", alias, path)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Replace an existing registration")
	return cmd
}

func newVaultsUnregisterCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unregister <alias>",
		Short: "Remove a vault alias (does not delete vault contents)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			alias := args[0]
			reg, err := config.LoadRegistry()
			if err != nil {
				return err
			}
			if _, ok := reg.Vaults[alias]; !ok {
				return fmt.Errorf("alias %q not found", alias)
			}
			delete(reg.Vaults, alias)
			if err := config.WriteRegistry(reg); err != nil {
				return err
			}
			fmt.Printf("unregistered: %s\n", alias)
			return nil
		},
	}
}

func newVaultsRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <old> <new>",
		Short: "Rename a registered alias",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			old, newn := args[0], args[1]
			if strings.ContainsAny(newn, "/\\.") {
				return fmt.Errorf("invalid alias %q: no slashes or dots", newn)
			}
			reg, err := config.LoadRegistry()
			if err != nil {
				return err
			}
			entry, ok := reg.Vaults[old]
			if !ok {
				return fmt.Errorf("alias %q not found", old)
			}
			if _, exists := reg.Vaults[newn]; exists {
				return fmt.Errorf("alias %q already registered", newn)
			}
			delete(reg.Vaults, old)
			reg.Vaults[newn] = entry
			if err := config.WriteRegistry(reg); err != nil {
				return err
			}
			fmt.Printf("renamed: %s → %s\n", old, newn)
			return nil
		},
	}
}

func newVaultsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <alias>",
		Short: "Show the path registered to an alias",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			reg, err := config.LoadRegistry()
			if err != nil {
				return err
			}
			entry, ok := reg.Vaults[args[0]]
			if !ok {
				return fmt.Errorf("alias %q not found", args[0])
			}
			fmt.Printf("alias: %s\npath:  %s\n", args[0], entry.Path)
			return nil
		},
	}
}

// --- template (inspection) ---

func newTemplateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "template",
		Short: "Inspect templates (resolved across the search path)",
	}

	var vaultRefFlag string
	buildResolver := func() *templates.Resolver {
		if vaultRefFlag == "" {
			return mkResolver()
		}
		vp, err := config.ResolveRef(vaultRefFlag)
		if err != nil {
			return mkResolver()
		}
		return mkResolver(templates.VaultOverridesDir(vp))
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List available templates",
		RunE: func(_ *cobra.Command, _ []string) error {
			names, err := buildResolver().List()
			if err != nil {
				return err
			}
			sort.Strings(names)
			for _, n := range names {
				fmt.Println(n)
			}
			return nil
		},
	}
	listCmd.Flags().StringVar(&vaultRefFlag, "vault", "", "Resolve inside a vault's override path (alias or path)")

	var showFormat string
	var decorate bool
	showCmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show a resolved template (after inheritance and brace expansion)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			t, err := buildResolver().Resolve(args[0])
			if err != nil {
				return err
			}
			return writeTemplateShow(os.Stdout, t, showFormat, decorate)
		},
	}
	showCmd.Flags().StringVar(&vaultRefFlag, "vault", "", "Resolve inside a vault's override path (alias or path)")
	showCmd.Flags().StringVar(&showFormat, "format", "text", "Output format: text | yaml | json")
	showCmd.Flags().BoolVar(&decorate, "decorate", false, "Use colors and Unicode glyphs (text format only)")

	sourcesCmd := &cobra.Command{
		Use:   "sources <name>",
		Short: "Show every location in the search path where this template is defined",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			hits := buildResolver().Sources(args[0])
			if len(hits) == 0 {
				return fmt.Errorf("template %q not found in search path", args[0])
			}
			for _, h := range hits {
				fmt.Println(h)
			}
			return nil
		},
	}
	sourcesCmd.Flags().StringVar(&vaultRefFlag, "vault", "", "Resolve inside a vault's override path (alias or path)")

	pathCmd := &cobra.Command{
		Use:   "path",
		Short: "Show the template search path in priority order",
		Long: `Print every directory mega-mem will consult when resolving a template name,
in priority order (first hit wins). Indicates which paths currently exist.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			res := buildResolver()
			for i, p := range res.SearchPath {
				if p == "" {
					continue
				}
				status := "missing"
				if info, err := os.Stat(p); err == nil && info.IsDir() {
					status = "exists"
				}
				fmt.Printf("%d. %s  [%s]\n", i+1, p, status)
			}
			return nil
		},
	}
	pathCmd.Flags().StringVar(&vaultRefFlag, "vault", "", "Include the vault's override dir in the path")

	cmd.AddCommand(listCmd, showCmd, sourcesCmd, pathCmd)
	return cmd
}

func writeTemplateShow(w io.Writer, t *templates.Template, format string, decorate bool) error {
	switch strings.ToLower(format) {
	case "yaml":
		data, err := yaml.Marshal(t)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(t)
	case "", "text":
		return writeTemplatePretty(w, t, decorate)
	default:
		return fmt.Errorf("unknown format %q (valid: text, yaml, json)", format)
	}
}

func writeTemplatePretty(w io.Writer, t *templates.Template, decorate bool) error {
	// decorate is reserved for future ANSI/tree-glyph polish; current pretty
	// view is plain enough to work in any terminal or CI log.
	_ = decorate

	fmt.Fprintf(w, "Template: %s\n", t.Name)
	if t.Description != "" {
		fmt.Fprintf(w, "Description: %s\n", t.Description)
	}
	if t.Source != "" {
		fmt.Fprintf(w, "Source: %s\n", t.Source)
	}
	fmt.Fprintln(w)

	if len(t.Inherit) == 0 {
		fmt.Fprintln(w, "Inherits: (none)")
	} else {
		fmt.Fprintf(w, "Inherits: %s\n", strings.Join(t.Inherit, ", "))
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "Folders (%d):\n", len(t.Folders))
	if len(t.Folders) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, f := range t.Folders {
		fmt.Fprintf(w, "  %s/\n", f)
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "Files (%d):\n", len(t.Files))
	if len(t.Files) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, f := range t.Files {
		tail := ""
		switch {
		case f.Source != "":
			tail = "  ← " + f.Source
		case f.Content != "":
			tail = "  (inline content)"
		}
		fmt.Fprintf(w, "  %s%s\n", f.Path, tail)
	}

	if len(t.Children) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Children (%d):\n", len(t.Children))
		for _, c := range t.Children {
			excl := ""
			if len(c.Exclude) > 0 {
				excl = fmt.Sprintf(" (excluding %s)", strings.Join(c.Exclude, ", "))
			}
			fmt.Fprintf(w, "  %s/ → template %q%s\n", c.Parent, c.Template, excl)
		}
	}

	return nil
}
