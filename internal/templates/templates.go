// Package templates loads folder-structure templates from a filesystem search
// path and resolves them with inheritance, brace expansion, and children
// declarations validated at parse time.
package templates

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Template declares a folder structure and, optionally, files to materialize
// and child templates to apply recursively.
type Template struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Inherit     []string `yaml:"inherit,omitempty"`
	Folders     []string `yaml:"folders,omitempty"`
	Files       []File   `yaml:"files,omitempty"`
	Children    []Child  `yaml:"children,omitempty"`

	// Source is the absolute path to the YAML file this template was loaded
	// from. Populated at load; not serialized.
	Source string `yaml:"-"`
}

// File declares a file to materialize into a scaffolded target.
type File struct {
	Path       string `yaml:"path"`                  // relative to target
	Source     string `yaml:"source,omitempty"`      // relative to this template's YAML file
	Content    string `yaml:"content,omitempty"`     // inline literal
	Mode       string `yaml:"mode,omitempty"`        // octal, e.g. "0644"
	OnConflict string `yaml:"on_conflict,omitempty"` // "skip" (default) | "overwrite" | "error"
}

// Child declares a recursion rule: scan `parent` for subdirectories and apply
// `template` to each, skipping any named in `exclude`.
type Child struct {
	Parent   string   `yaml:"parent"`
	Template string   `yaml:"template"`
	Exclude  []string `yaml:"exclude,omitempty"`
}

// Resolver loads and merges templates from a search path.
type Resolver struct {
	SearchPath []string
}

// NewResolver builds a Resolver with the default search path, plus any
// additional entries pre-pended (higher priority).
func NewResolver(extraDirs ...string) *Resolver {
	return &Resolver{SearchPath: append(append([]string{}, extraDirs...), defaultSearchPath()...)}
}

// Resolve loads a template by name and merges any inherited templates.
// Folder brace expansion is applied after the merge.
func (r *Resolver) Resolve(name string) (*Template, error) {
	visiting := map[string]bool{}
	return r.resolveOne(name, visiting)
}

// resolveOne is the recursive worker used for inheritance resolution with
// cycle detection.
func (r *Resolver) resolveOne(name string, visiting map[string]bool) (*Template, error) {
	if visiting[name] {
		return nil, fmt.Errorf("template inheritance cycle detected at %q", name)
	}
	visiting[name] = true
	defer delete(visiting, name)

	raw, err := r.loadRaw(name)
	if err != nil {
		return nil, err
	}

	merged := &Template{
		Name:   raw.Name,
		Source: raw.Source,
	}
	// Parents first, then child — child wins on conflicts.
	for _, parent := range raw.Inherit {
		p, err := r.resolveOne(parent, visiting)
		if err != nil {
			return nil, fmt.Errorf("inherit %q from %q: %w", parent, name, err)
		}
		mergeInto(merged, p)
	}
	mergeInto(merged, raw)
	merged.Name = raw.Name // name always wins from the outer template
	merged.Description = raw.Description
	merged.Source = raw.Source

	// Expand braces once on the final merged folder list.
	merged.Folders = dedupStrings(ExpandMany(merged.Folders))

	// Validate children references now that we have the final template.
	if err := r.validateChildren(merged); err != nil {
		return nil, fmt.Errorf("template %q: %w", name, err)
	}

	return merged, nil
}

// loadRaw reads a single template YAML from the search path without following
// inheritance.
func (r *Resolver) loadRaw(name string) (*Template, error) {
	path, err := r.Find(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read template %s: %w", path, err)
	}
	t := &Template{}
	if err := yaml.Unmarshal(data, t); err != nil {
		return nil, fmt.Errorf("parse template %s: %w", path, err)
	}
	if t.Name == "" {
		t.Name = name
	}
	t.Source = path
	return t, nil
}

// Find returns the absolute path of the first template YAML named <name>.yaml
// in the search path.
func (r *Resolver) Find(name string) (string, error) {
	filename := name + ".yaml"
	for _, dir := range r.SearchPath {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, filename)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("stat %s: %w", candidate, err)
		}
	}
	return "", fmt.Errorf("template %q not found in search path: %s", name, strings.Join(r.SearchPath, ":"))
}

// Sources returns every path in the search path where a template named <name>
// would resolve. Useful for `mega-mem template sources`.
func (r *Resolver) Sources(name string) []string {
	filename := name + ".yaml"
	var hits []string
	for _, dir := range r.SearchPath {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, filename)
		if _, err := os.Stat(candidate); err == nil {
			hits = append(hits, candidate)
		}
	}
	return hits
}

// List returns the names of all templates visible across the search path.
func (r *Resolver) List() ([]string, error) {
	seen := map[string]bool{}
	for _, dir := range r.SearchPath {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			n := e.Name()
			if !strings.HasSuffix(n, ".yaml") {
				continue
			}
			seen[strings.TrimSuffix(n, ".yaml")] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	return out, nil
}

// validateChildren ensures every children[].template resolves in the search
// path. Runs after inheritance merge.
func (r *Resolver) validateChildren(t *Template) error {
	for _, c := range t.Children {
		if c.Parent == "" {
			return fmt.Errorf("children entry missing parent")
		}
		if c.Template == "" {
			return fmt.Errorf("children entry for parent %q missing template", c.Parent)
		}
		if _, err := r.Find(c.Template); err != nil {
			return fmt.Errorf("children references template %q which is unresolvable: %w", c.Template, err)
		}
	}
	return nil
}

// mergeInto merges src into dst. Folders append; files keyed by path; children
// keyed by parent. The caller is responsible for final dedup and brace expansion.
func mergeInto(dst, src *Template) {
	dst.Folders = append(dst.Folders, src.Folders...)

	fileIdx := map[string]int{}
	for i, f := range dst.Files {
		fileIdx[f.Path] = i
	}
	for _, f := range src.Files {
		if i, ok := fileIdx[f.Path]; ok {
			dst.Files[i] = f
		} else {
			dst.Files = append(dst.Files, f)
			fileIdx[f.Path] = len(dst.Files) - 1
		}
	}

	childIdx := map[string]int{}
	for i, c := range dst.Children {
		childIdx[c.Parent] = i
	}
	for _, c := range src.Children {
		if i, ok := childIdx[c.Parent]; ok {
			dst.Children[i] = c
		} else {
			dst.Children = append(dst.Children, c)
			childIdx[c.Parent] = len(dst.Children) - 1
		}
	}
}

func dedupStrings(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// defaultSearchPath assembles the filesystem search path in priority order.
// Earlier entries win over later ones when resolving a template by name.
func defaultSearchPath() []string {
	paths := []string{}
	if v := os.Getenv("MEGAMEM_TEMPLATES_DIR"); v != "" {
		paths = append(paths, v)
	}
	if home, err := os.UserHomeDir(); err == nil {
		if cfg := os.Getenv("XDG_CONFIG_HOME"); cfg != "" {
			paths = append(paths, filepath.Join(cfg, "mega-mem", "templates"))
		} else {
			paths = append(paths, filepath.Join(home, ".config", "mega-mem", "templates"))
		}
		if data := os.Getenv("XDG_DATA_HOME"); data != "" {
			paths = append(paths, filepath.Join(data, "mega-mem", "templates"))
		} else {
			paths = append(paths, filepath.Join(home, ".local", "share", "mega-mem", "templates"))
		}
	}
	paths = append(paths, "/usr/local/share/mega-mem/templates")
	paths = append(paths, "/usr/share/mega-mem/templates")
	if exe, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), "..", "share", "mega-mem", "templates", "default"))
	}
	return paths
}

// VaultOverridesDir returns <vault>/.mega-mem/templates/ — the per-vault
// override directory, prepended to the search path when operating inside a
// specific vault.
func VaultOverridesDir(vaultPath string) string {
	return filepath.Join(vaultPath, ".mega-mem", "templates")
}
