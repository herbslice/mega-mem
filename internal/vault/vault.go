// Package vault scaffolds vaults and associated metadata.
package vault

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/herbslice/mega-mem/internal/config"
	"github.com/herbslice/mega-mem/internal/scaffold"
	"github.com/herbslice/mega-mem/internal/templates"
)

// InitOpts controls Init.
type InitOpts struct {
	Force        bool
	DryRun       bool
	RootTemplate string // default: "vault-root"
	TemplatesDir string // prepended to the template search path if non-empty
	Git          bool   // run `git init` and write a starter .gitignore if the vault isn't already a git repo
}

// Init creates a new vault at path. If the directory exists and is empty, it
// is used in place; if it exists and is non-empty, the run is refused unless
// --force. The root template is applied and .mega-mem.yaml is written.
func Init(path string, opts InitOpts) error {
	if opts.RootTemplate == "" {
		opts.RootTemplate = "vault-root"
	}

	info, err := os.Stat(path)
	switch {
	case err == nil:
		if !info.IsDir() {
			return fmt.Errorf("%s exists and is not a directory", path)
		}
		// Non-empty directories are OK: scaffold is idempotent and .mega-mem.yaml
		// is guarded below. This supports "adopt" workflows where the user
		// wants to turn an existing folder into a vault.
	case errors.Is(err, os.ErrNotExist):
		if !opts.DryRun {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return fmt.Errorf("create vault dir: %w", err)
			}
		}
	default:
		return fmt.Errorf("stat %s: %w", path, err)
	}

	extraDirs := []string{templates.VaultOverridesDir(path)}
	if opts.TemplatesDir != "" {
		extraDirs = append([]string{opts.TemplatesDir}, extraDirs...)
	}
	res := templates.NewResolver(extraDirs...)
	tpl, err := res.Resolve(opts.RootTemplate)
	if err != nil {
		// No templates installed: init is still useful — create the path and
		// write .mega-mem.yaml so scaffold can run later once templates exist.
		if !opts.DryRun {
			if err := writeVaultConfig(path, opts.Force); err != nil {
				return err
			}
		}
		fmt.Fprintf(os.Stderr, "note: no '%s' template resolved; created empty vault at %s\n", opts.RootTemplate, path)
		return nil
	}

	plan, err := scaffold.Compute(res, tpl, path, scaffold.Options{Force: opts.Force})
	if err != nil {
		return err
	}

	if opts.DryRun {
		if err := scaffold.Format(os.Stdout, plan, "text", false); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "(dry-run — no changes made)\n")
		return nil
	}

	if err := writeVaultConfig(path, opts.Force); err != nil {
		return err
	}

	if err := scaffold.Apply(plan); err != nil {
		return err
	}

	if opts.Git {
		if err := initGit(path, opts.Force); err != nil {
			return fmt.Errorf("init git: %w", err)
		}
	}

	fmt.Printf("Initialized vault at %s (%d items)\n", path, len(plan.Items))
	return nil
}

// gitignoreTemplate is the starter .gitignore written by `init --git`. It
// excludes per-machine state (search index, embedding cache) and common
// sync-conflict / merge artifacts, but keeps the markdown vault contents
// version-controlled.
const gitignoreTemplate = `# Per-machine search index and runtime state (rebuilds from markdown)
.mega-mem/index.sqlite
.mega-mem/index.sqlite-shm
.mega-mem/index.sqlite-wal
.mega-mem/cache/

# Sync-conflict files (Syncthing, Nextcloud)
*.sync-conflict-*.md
*.sync-conflict-*

# Git merge artifacts
*.orig

# Editor backups
*.swp
*~
.DS_Store
`

// initGit runs `git init` if .git is missing and writes a starter
// .gitignore template. Idempotent: existing .gitignore is left in place
// unless force is true.
func initGit(path string, force bool) error {
	gitDir := filepath.Join(path, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat .git: %w", err)
		}
		cmd := exec.Command("git", "init", "--initial-branch=main", path)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git init: %w (output: %s)", err, string(out))
		}
	}
	gitignorePath := filepath.Join(path, ".gitignore")
	if _, err := os.Stat(gitignorePath); err == nil && !force {
		return nil
	}
	if err := os.WriteFile(gitignorePath, []byte(gitignoreTemplate), 0o644); err != nil {
		return fmt.Errorf("write .gitignore: %w", err)
	}
	return nil
}

// writeVaultConfig writes <path>/.mega-mem.yaml. If it already exists and
// force is false, leaves the existing file in place (common on re-runs and
// when adopting a pre-existing directory).
func writeVaultConfig(path string, force bool) error {
	yamlPath := filepath.Join(path, ".mega-mem.yaml")
	if _, err := os.Stat(yamlPath); err == nil && !force {
		return nil
	}
	cfg := &config.Vault{VaultID: filepath.Base(path)}
	if err := config.WriteVault(path, cfg); err != nil {
		return fmt.Errorf("write vault config: %w", err)
	}
	return nil
}
