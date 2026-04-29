package scaffold

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// Format renders a Plan into w in one of several forms.
//
//	format="text" → flat +/~/- list (default)
//	format="json" → structured JSON
//	tree=true    → tree-shaped text view (overrides format)
func Format(w io.Writer, plan *Plan, format string, tree bool) error {
	if tree {
		return formatTree(w, plan)
	}
	switch strings.ToLower(format) {
	case "", "text":
		return formatText(w, plan)
	case "json":
		return formatJSON(w, plan)
	default:
		return fmt.Errorf("unknown format %q (valid: text, json)", format)
	}
}

func formatText(w io.Writer, plan *Plan) error {
	creates := 0
	skips := 0
	extras := 0

	sorted := append([]Item(nil), plan.Items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Path < sorted[j].Path
	})

	for _, it := range sorted {
		prefix := "?"
		switch it.Action {
		case ActionCreate:
			prefix = "+"
			creates++
		case ActionSkip:
			prefix = "~"
			skips++
		case ActionExtra:
			prefix = "-"
			extras++
		}
		suffix := ""
		if it.Kind == KindFolder {
			suffix = "/"
		}
		line := fmt.Sprintf("%s %s%s", prefix, it.Path, suffix)
		if it.Reason != "" {
			line += " (" + it.Reason + ")"
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "legend: + create, ~ skip, - extra   (target: %s; template: %s)\n", plan.Target, plan.Template)
	fmt.Fprintf(w, "summary: %d to create, %d skipped, %d extras\n", creates, skips, extras)
	return nil
}

func formatJSON(w io.Writer, plan *Plan) error {
	// Shape a flat JSON so it's easy to pipe into jq.
	type jsonItem struct {
		Path     string `json:"path"`
		Kind     string `json:"kind"`
		Action   string `json:"action"`
		Template string `json:"template"`
		Reason   string `json:"reason,omitempty"`
	}
	out := struct {
		Target   string     `json:"target"`
		Template string     `json:"template"`
		Items    []jsonItem `json:"items"`
	}{
		Target:   plan.Target,
		Template: plan.Template,
	}
	for _, it := range plan.Items {
		out.Items = append(out.Items, jsonItem{
			Path:     it.Path,
			Kind:     string(it.Kind),
			Action:   string(it.Action),
			Template: it.Template,
			Reason:   it.Reason,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// formatTree groups items into a tree rooted at the target. Creates appear
// with "+", skips with "~", extras with "-". Intermediate directories are
// shown uncolored.
func formatTree(w io.Writer, plan *Plan) error {
	// Build a tree of paths → action marker (if any).
	type node struct {
		name     string
		action   Action
		children map[string]*node
		hasAct   bool
	}
	root := &node{name: filepath.Base(plan.Target), children: map[string]*node{}}
	for _, it := range plan.Items {
		parts := strings.Split(filepath.ToSlash(it.Path), "/")
		cur := root
		for i, part := range parts {
			child, ok := cur.children[part]
			if !ok {
				child = &node{name: part, children: map[string]*node{}}
				cur.children[part] = child
			}
			if i == len(parts)-1 {
				child.action = it.Action
				child.hasAct = true
			}
			cur = child
		}
	}

	var walk func(n *node, prefix string, isLast bool, isRoot bool)
	walk = func(n *node, prefix string, isLast bool, isRoot bool) {
		connector := "├── "
		if isLast {
			connector = "└── "
		}
		marker := "  "
		if n.hasAct {
			switch n.action {
			case ActionCreate:
				marker = "+ "
			case ActionSkip:
				marker = "~ "
			case ActionExtra:
				marker = "- "
			}
		}
		if isRoot {
			fmt.Fprintln(w, n.name+"/")
		} else {
			fmt.Fprintf(w, "%s%s%s%s\n", prefix, connector, marker, n.name)
		}

		keys := make([]string, 0, len(n.children))
		for k := range n.children {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			last := i == len(keys)-1
			childPrefix := prefix
			if !isRoot {
				if isLast {
					childPrefix += "    "
				} else {
					childPrefix += "│   "
				}
			}
			walk(n.children[k], childPrefix, last, false)
		}
	}
	walk(root, "", true, true)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "legend: + create, ~ skip, - extra")
	return nil
}
