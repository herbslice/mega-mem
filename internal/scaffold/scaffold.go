// Package scaffold computes and applies a Plan that materializes a template
// into a target directory. Safe by default: never deletes, never clobbers
// existing files unless explicitly forced.
package scaffold

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/herbslice/mega-mem/internal/templates"
)

// Action is a per-item outcome in a scaffold Plan.
type Action string

const (
	ActionCreate Action = "create"
	ActionSkip   Action = "skip"
	ActionExtra  Action = "extra"
)

// Kind identifies whether a plan item is a folder or file.
type Kind string

const (
	KindFolder Kind = "folder"
	KindFile   Kind = "file"
)

// Item is a single planned operation.
type Item struct {
	Path     string      // path relative to the Plan.Target
	Abs      string      // absolute path
	Kind     Kind        // folder or file
	Action   Action      // create | skip | extra
	Template string      // template whose declaration produced this item
	Source   string      // absolute path of the source file (for files with source:)
	Content  []byte      // inline literal content (for files with content:)
	Mode     os.FileMode // permissions for files
	Reason   string      // why skipped; included in output
}

// Plan is the computed set of changes a scaffold run would apply.
type Plan struct {
	Target   string
	Template string
	Items    []Item
}

// Options controls Compute and Apply behavior.
type Options struct {
	Force     bool // overwrite existing files
	Diff      bool // include extras (things present in target but not in template)
	NoRecurse bool // skip children: declarations
}

// Compute walks the resolved template + target and returns a Plan. It does
// not touch the filesystem except to stat.
func Compute(res *templates.Resolver, tpl *templates.Template, target string, opts Options) (*Plan, error) {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return nil, fmt.Errorf("resolve target %s: %w", target, err)
	}

	plan := &Plan{Target: absTarget, Template: tpl.Name}

	// Folders
	for _, f := range tpl.Folders {
		rel := filepath.Clean(f)
		abs := filepath.Join(absTarget, rel)
		kind := KindFolder
		action := ActionCreate
		if info, err := os.Stat(abs); err == nil {
			if info.IsDir() {
				continue // already exists; no noise
			}
			action = ActionSkip
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("stat %s: %w", abs, err)
		}
		plan.Items = append(plan.Items, Item{
			Path:     rel,
			Abs:      abs,
			Kind:     kind,
			Action:   action,
			Template: tpl.Name,
			Reason:   reasonIfSkip(action, "path exists and is not a directory"),
		})
	}

	// Files
	for _, f := range tpl.Files {
		rel := filepath.Clean(f.Path)
		abs := filepath.Join(absTarget, rel)

		// Resolve the expected content (from source file or inline).
		var expected []byte
		var source string
		switch {
		case f.Source != "":
			if tpl.Source == "" {
				return nil, fmt.Errorf("template %q has file with source %q but no loaded Source path", tpl.Name, f.Source)
			}
			source = filepath.Join(filepath.Dir(tpl.Source), f.Source)
			data, err := os.ReadFile(source)
			if err != nil {
				return nil, fmt.Errorf("read source %s: %w", source, err)
			}
			expected = data
		case f.Content != "":
			expected = []byte(f.Content)
		default:
			return nil, fmt.Errorf("template %q file %q has neither source nor content", tpl.Name, f.Path)
		}

		existing, readErr := os.ReadFile(abs)
		exists := readErr == nil
		if !exists && !errors.Is(readErr, os.ErrNotExist) {
			return nil, fmt.Errorf("read %s: %w", abs, readErr)
		}

		// If the file exists with identical content, it's already in the
		// desired state — skip silently (do not appear in the plan).
		if exists && bytes.Equal(existing, expected) {
			continue
		}

		item := Item{
			Path:     rel,
			Abs:      abs,
			Kind:     KindFile,
			Template: tpl.Name,
			Mode:     fileMode(f.Mode),
			Source:   source,
			Content:  expected,
		}

		switch {
		case !exists:
			item.Action = ActionCreate
		case opts.Force, strings.EqualFold(f.OnConflict, "overwrite"):
			item.Action = ActionCreate
		case strings.EqualFold(f.OnConflict, "error"):
			return nil, fmt.Errorf("file %s already exists (template policy: error)", rel)
		default:
			item.Action = ActionSkip
			item.Reason = "file exists with different content; use --force to overwrite"
		}
		plan.Items = append(plan.Items, item)
	}

	// Children recursion
	if !opts.NoRecurse {
		for _, c := range tpl.Children {
			parentAbs := filepath.Join(absTarget, c.Parent)
			entries, err := os.ReadDir(parentAbs)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue // parent folder doesn't exist yet; nothing to recurse into
				}
				return nil, fmt.Errorf("read %s: %w", parentAbs, err)
			}
			excludeSet := map[string]bool{}
			for _, ex := range c.Exclude {
				excludeSet[ex] = true
			}
			childTpl, err := res.Resolve(c.Template)
			if err != nil {
				return nil, fmt.Errorf("resolve child template %q: %w", c.Template, err)
			}
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				name := e.Name()
				if strings.HasPrefix(name, ".") {
					continue
				}
				if excludeSet[name] {
					continue
				}
				sub := filepath.Join(parentAbs, name)
				childPlan, err := Compute(res, childTpl, sub, opts)
				if err != nil {
					return nil, err
				}
				plan.Items = append(plan.Items, childPlan.Items...)
			}
		}
	}

	// Diff mode: look for extras in the target not declared by the template.
	if opts.Diff {
		extras, err := findExtras(absTarget, tpl, opts.NoRecurse)
		if err != nil {
			return nil, err
		}
		plan.Items = append(plan.Items, extras...)
	}

	return plan, nil
}

// SkippedError is returned by Apply when the run completed successfully but
// one or more items were skipped (typically existing files without --force).
// Callers map this to a non-zero exit code (convention: 3).
type SkippedError struct {
	N int
}

func (e *SkippedError) Error() string {
	return fmt.Sprintf("completed with %d skipped item(s); see stderr for details", e.N)
}

// Apply executes a Plan against the filesystem. Returns SkippedError (wrapped
// as a regular error) if any items were skipped; nil on a fully-clean apply.
func Apply(plan *Plan) error {
	// Sort so folders are created before files that live in them.
	sorted := append([]Item(nil), plan.Items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		// folders first
		if sorted[i].Kind != sorted[j].Kind {
			return sorted[i].Kind == KindFolder
		}
		return sorted[i].Path < sorted[j].Path
	})

	skipped := 0
	for _, it := range sorted {
		switch it.Action {
		case ActionCreate:
			switch it.Kind {
			case KindFolder:
				if err := os.MkdirAll(it.Abs, 0o755); err != nil {
					return fmt.Errorf("mkdir %s: %w", it.Abs, err)
				}
			case KindFile:
				if err := os.MkdirAll(filepath.Dir(it.Abs), 0o755); err != nil {
					return fmt.Errorf("mkdir parent of %s: %w", it.Abs, err)
				}
				// Compute stored the resolved content on Item.Content; no re-read.
				if err := os.WriteFile(it.Abs, it.Content, it.Mode); err != nil {
					return fmt.Errorf("write %s: %w", it.Abs, err)
				}
			}
		case ActionSkip:
			fmt.Fprintf(os.Stderr, "skip: %s (%s)\n", it.Path, it.Reason)
			skipped++
		case ActionExtra:
			// informational only; never modify extras
		}
	}
	if skipped > 0 {
		return &SkippedError{N: skipped}
	}
	return nil
}

// findExtras walks the target directory tree and returns items that the
// template does not declare, for --diff output.
func findExtras(target string, tpl *templates.Template, noRecurse bool) ([]Item, error) {
	declaredFolders := map[string]bool{}
	declaredFiles := map[string]bool{}
	addWithParents := func(p string) {
		cleaned := filepath.Clean(p)
		declaredFolders[cleaned] = true
		for dir := filepath.Dir(cleaned); dir != "." && dir != "/" && dir != ""; dir = filepath.Dir(dir) {
			declaredFolders[dir] = true
		}
	}
	for _, f := range tpl.Folders {
		addWithParents(f)
	}
	for _, f := range tpl.Files {
		cleaned := filepath.Clean(f.Path)
		declaredFiles[cleaned] = true
		if dir := filepath.Dir(cleaned); dir != "." && dir != "/" && dir != "" {
			addWithParents(dir)
		}
	}
	// Children parents are implicit containers; don't mark their sub-contents as extras.
	childParents := map[string]bool{}
	if !noRecurse {
		for _, c := range tpl.Children {
			childParents[filepath.Clean(c.Parent)] = true
		}
	}

	var extras []Item
	err := filepath.WalkDir(target, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if p == target {
			return nil
		}
		rel, err := filepath.Rel(target, p)
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		// Skip content under any child parent — children recursion handles those.
		for parent := range childParents {
			if rel == parent || strings.HasPrefix(rel, parent+string(filepath.Separator)) {
				return nil
			}
		}
		if d.IsDir() {
			if !declaredFolders[rel] {
				extras = append(extras, Item{
					Path:   rel,
					Abs:    p,
					Kind:   KindFolder,
					Action: ActionExtra,
					Reason: "not in template",
				})
			}
			return nil
		}
		if !declaredFiles[rel] {
			extras = append(extras, Item{
				Path:   rel,
				Abs:    p,
				Kind:   KindFile,
				Action: ActionExtra,
				Reason: "not in template",
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return extras, nil
}

func fileMode(s string) os.FileMode {
	if s == "" {
		return 0o644
	}
	n, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0o644
	}
	return os.FileMode(n)
}

func reasonIfSkip(action Action, reason string) string {
	if action == ActionSkip {
		return reason
	}
	return ""
}
