package templates

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func writeYAML(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindFirstHitWins(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	writeYAML(t, a, "thing", "name: thing\nfolders: [from-a]\n")
	writeYAML(t, b, "thing", "name: thing\nfolders: [from-b]\n")

	r := &Resolver{SearchPath: []string{a, b}}
	got, err := r.Find("thing")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(a, "thing.yaml") {
		t.Errorf("expected hit under %s; got %s", a, got)
	}

	r2 := &Resolver{SearchPath: []string{b, a}}
	got, err = r2.Find("thing")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(b, "thing.yaml") {
		t.Errorf("expected hit under %s; got %s", b, got)
	}
}

func TestFindMissing(t *testing.T) {
	r := &Resolver{SearchPath: []string{t.TempDir()}}
	if _, err := r.Find("nope"); err == nil {
		t.Error("expected error for missing template")
	}
}

func TestFindSkipsEmptyDirEntry(t *testing.T) {
	d := t.TempDir()
	writeYAML(t, d, "x", "name: x\n")
	r := &Resolver{SearchPath: []string{"", d}}
	if _, err := r.Find("x"); err != nil {
		t.Errorf("empty dir entry should be skipped, got: %v", err)
	}
}

func TestResolveSimple(t *testing.T) {
	d := t.TempDir()
	writeYAML(t, d, "simple", "name: simple\nfolders:\n  - one\n  - two\n")
	r := &Resolver{SearchPath: []string{d}}
	tpl, err := r.Resolve("simple")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tpl.Folders, []string{"one", "two"}) {
		t.Errorf("folders = %v", tpl.Folders)
	}
	if tpl.Source != filepath.Join(d, "simple.yaml") {
		t.Errorf("source = %q", tpl.Source)
	}
}

func TestResolveBraceExpansion(t *testing.T) {
	d := t.TempDir()
	writeYAML(t, d, "br", "name: br\nfolders:\n  - 'pre/{a,b}'\n")
	r := &Resolver{SearchPath: []string{d}}
	tpl, err := r.Resolve("br")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tpl.Folders, []string{"pre/a", "pre/b"}) {
		t.Errorf("folders = %v", tpl.Folders)
	}
}

func TestResolveInheritFoldersAppend(t *testing.T) {
	d := t.TempDir()
	writeYAML(t, d, "base", "name: base\nfolders: [a, b]\n")
	writeYAML(t, d, "child", "name: child\ninherit: [base]\nfolders: [c]\n")
	r := &Resolver{SearchPath: []string{d}}
	tpl, err := r.Resolve("child")
	if err != nil {
		t.Fatal(err)
	}
	// Parent first, then child; dedup preserves first-seen order.
	if !reflect.DeepEqual(tpl.Folders, []string{"a", "b", "c"}) {
		t.Errorf("folders = %v", tpl.Folders)
	}
}

func TestResolveInheritDedups(t *testing.T) {
	d := t.TempDir()
	writeYAML(t, d, "base", "name: base\nfolders: [a, b]\n")
	writeYAML(t, d, "child", "name: child\ninherit: [base]\nfolders: [b, c]\n")
	r := &Resolver{SearchPath: []string{d}}
	tpl, err := r.Resolve("child")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tpl.Folders, []string{"a", "b", "c"}) {
		t.Errorf("folders = %v", tpl.Folders)
	}
}

func TestResolveInheritMultipleParents(t *testing.T) {
	d := t.TempDir()
	writeYAML(t, d, "p1", "name: p1\nfolders: [a]\n")
	writeYAML(t, d, "p2", "name: p2\nfolders: [b]\n")
	writeYAML(t, d, "child", "name: child\ninherit: [p1, p2]\nfolders: [c]\n")
	r := &Resolver{SearchPath: []string{d}}
	tpl, err := r.Resolve("child")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tpl.Folders, []string{"a", "b", "c"}) {
		t.Errorf("folders = %v", tpl.Folders)
	}
}

func TestResolveChildOverridesFile(t *testing.T) {
	d := t.TempDir()
	writeYAML(t, d, "base", "name: base\nfiles:\n  - {path: x.md, content: \"from base\"}\n")
	writeYAML(t, d, "child", "name: child\ninherit: [base]\nfiles:\n  - {path: x.md, content: \"from child\"}\n")
	r := &Resolver{SearchPath: []string{d}}
	tpl, err := r.Resolve("child")
	if err != nil {
		t.Fatal(err)
	}
	if len(tpl.Files) != 1 {
		t.Fatalf("expected 1 merged file, got %d", len(tpl.Files))
	}
	if tpl.Files[0].Content != "from child" {
		t.Errorf("file content = %q; want %q", tpl.Files[0].Content, "from child")
	}
}

func TestResolveChildOverridesChildren(t *testing.T) {
	d := t.TempDir()
	writeYAML(t, d, "leaf", "name: leaf\n")
	writeYAML(t, d, "leaf2", "name: leaf2\n")
	writeYAML(t, d, "base", "name: base\nchildren:\n  - {parent: subs, template: leaf}\n")
	writeYAML(t, d, "child", "name: child\ninherit: [base]\nchildren:\n  - {parent: subs, template: leaf2}\n")
	r := &Resolver{SearchPath: []string{d}}
	tpl, err := r.Resolve("child")
	if err != nil {
		t.Fatal(err)
	}
	if len(tpl.Children) != 1 {
		t.Fatalf("expected 1 merged child, got %d", len(tpl.Children))
	}
	if tpl.Children[0].Template != "leaf2" {
		t.Errorf("children[0].template = %q; want leaf2", tpl.Children[0].Template)
	}
}

func TestResolveCycle(t *testing.T) {
	d := t.TempDir()
	writeYAML(t, d, "a", "name: a\ninherit: [b]\n")
	writeYAML(t, d, "b", "name: b\ninherit: [a]\n")
	r := &Resolver{SearchPath: []string{d}}
	_, err := r.Resolve("a")
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error %q does not mention cycle", err.Error())
	}
}

func TestResolveValidatesChildTemplate(t *testing.T) {
	d := t.TempDir()
	writeYAML(t, d, "parent", "name: parent\nchildren:\n  - {parent: subs, template: missing}\n")
	r := &Resolver{SearchPath: []string{d}}
	if _, err := r.Resolve("parent"); err == nil {
		t.Error("expected error for unresolvable child template")
	}

	// Now provide it; resolution should succeed.
	writeYAML(t, d, "missing", "name: missing\nfolders: [x]\n")
	if _, err := r.Resolve("parent"); err != nil {
		t.Errorf("unexpected error after providing child template: %v", err)
	}
}

func TestResolveRejectsChildMissingFields(t *testing.T) {
	d := t.TempDir()
	writeYAML(t, d, "leaf", "name: leaf\n")

	writeYAML(t, d, "no-parent", "name: no-parent\nchildren:\n  - {template: leaf}\n")
	r := &Resolver{SearchPath: []string{d}}
	if _, err := r.Resolve("no-parent"); err == nil {
		t.Error("expected error for children entry missing parent")
	}

	writeYAML(t, d, "no-template", "name: no-template\nchildren:\n  - {parent: subs}\n")
	if _, err := r.Resolve("no-template"); err == nil {
		t.Error("expected error for children entry missing template")
	}
}

func TestList(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	writeYAML(t, a, "one", "name: one\n")
	writeYAML(t, a, "two", "name: two\n")
	writeYAML(t, b, "two", "name: two\n") // duplicate name across path
	writeYAML(t, b, "three", "name: three\n")
	// drop a non-yaml file in b that should be ignored
	if err := os.WriteFile(filepath.Join(b, "README.md"), []byte("ignore"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &Resolver{SearchPath: []string{a, b}}
	names, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, []string{"one", "three", "two"}) {
		t.Errorf("List() = %v", names)
	}
}

func TestListMissingDirIsNotError(t *testing.T) {
	d := t.TempDir()
	writeYAML(t, d, "x", "name: x\n")
	r := &Resolver{SearchPath: []string{filepath.Join(t.TempDir(), "nonexistent"), d}}
	names, err := r.List()
	if err != nil {
		t.Fatalf("missing dir should be ignored, got: %v", err)
	}
	if !reflect.DeepEqual(names, []string{"x"}) {
		t.Errorf("got %v", names)
	}
}

func TestSourcesReturnsAllHitsInOrder(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	writeYAML(t, a, "thing", "name: thing\n")
	writeYAML(t, b, "thing", "name: thing\n")
	r := &Resolver{SearchPath: []string{a, b}}
	hits := r.Sources("thing")
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0] != filepath.Join(a, "thing.yaml") || hits[1] != filepath.Join(b, "thing.yaml") {
		t.Errorf("hits = %v", hits)
	}
}

func TestVaultOverridesDir(t *testing.T) {
	got := VaultOverridesDir("/some/vault")
	want := filepath.Join("/some/vault", ".mega-mem", "templates")
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

func TestNewResolverPrependsExtras(t *testing.T) {
	r := NewResolver("/extra-1", "/extra-2")
	if len(r.SearchPath) < 2 || r.SearchPath[0] != "/extra-1" || r.SearchPath[1] != "/extra-2" {
		t.Errorf("extras not prepended: %v", r.SearchPath)
	}
}
