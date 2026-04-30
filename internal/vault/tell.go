package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/herbslice/mega-mem/internal/config"
)

// ErrNotInVault is returned by WhereAmI when no vault is found walking up
// from the starting directory.
var ErrNotInVault = errors.New("not in a vault")

// ErrUnregistered is returned when a vault directory is found but its path
// is not in the registry.
var ErrUnregistered = errors.New("vault is not registered")

// WhereAmI walks up from cwd looking for a directory containing
// .mega-mem.yaml. On match, it looks up the directory in the registry by
// path (with EvalSymlinks on both sides to handle bind mounts and macOS
// /private/). Returns:
//
//   - alias, path, nil             — registered vault containing cwd
//   - "",    path, ErrUnregistered — vault found but not in registry
//   - "",    "",   ErrNotInVault   — no .mega-mem.yaml found walking to /
func WhereAmI(cwd string) (alias, path string, err error) {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", "", fmt.Errorf("resolve cwd: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		abs = resolved
	}

	cur := abs
	for {
		marker := filepath.Join(cur, ".mega-mem.yaml")
		if _, err := os.Stat(marker); err == nil {
			return matchVault(cur)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", "", ErrNotInVault
		}
		cur = parent
	}
}

// matchVault looks up vaultPath in the registry, returning alias on success
// or ErrUnregistered with the path if no entry matches.
func matchVault(vaultPath string) (string, string, error) {
	reg, err := config.LoadRegistry()
	if err != nil {
		return "", vaultPath, fmt.Errorf("load registry: %w", err)
	}
	target, err := filepath.EvalSymlinks(vaultPath)
	if err != nil {
		target = vaultPath
	}
	for alias, entry := range reg.Vaults {
		regPath, err := filepath.EvalSymlinks(entry.Path)
		if err != nil {
			regPath = entry.Path
		}
		if regPath == target {
			return alias, vaultPath, nil
		}
	}
	return "", vaultPath, ErrUnregistered
}
