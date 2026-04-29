package scaffold

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/herbslice/mega-mem/internal/templates"
)

// makeTpl writes a stub YAML so tpl.Source resolves to a real path
// (Compute uses filepath.Dir(tpl.Source) to load files declared with `source:`),
// then returns the supplied template with Name and Source populated.
func makeTpl(t *testing.T, dir, name string, tpl *templates.Template) *templates.Template {
	t.Helper()
	p := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(p, []byte("name: "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tpl.Name = name
	tpl.Source = p
	return tpl
}

func TestComputeCreatesFolders(t *testing.T) {
	target := t.TempDir()
	tplDir := t.TempDir()
	tpl := makeTpl(t, tplDir, "t", &templates.Template{
		Folders: []string{"one", filepath.Join("two", "nested")},
	})
	plan, err := Compute(nil, tpl, target, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 2 {
		t.Fatalf("got %d items; want 2", len(plan.Items))
	}
	for _, it := range plan.Items {
		if it.Action != ActionCreate || it.Kind != KindFolder {
			t.Errorf("unexpected item: %+v", it)
		}
	}
}

func TestComputeFolderExistsIsSilentNoOp(t *testing.T) {
	target := t.TempDir()
	if err := os.Mkdir(filepath.Join(target, "exists"), 0o755); err != nil {
		t.Fatal(err)
	}
	tplDir := t.TempDir()
	tpl := makeTpl(t, tplDir, "t", &templates.Template{
		Folders: []string{"exists"},
	})
	plan, err := Compute(nil, tpl, target, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 0 {
		t.Errorf("expected silent no-op; got %d items", len(plan.Items))
	}
}

func TestComputeFolderPathOccupiedByFile(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "x"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	tplDir := t.TempDir()
	tpl := makeTpl(t, tplDir, "t", &templates.Template{
		Folders: []string{"x"},
	})
	plan, err := Compute(nil, tpl, target, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("got %d items", len(plan.Items))
	}
	if plan.Items[0].Action != ActionSkip {
		t.Errorf("got action %s; want Skip", plan.Items[0].Action)
	}
}

func TestComputeInlineFile(t *testing.T) {
	target := t.TempDir()
	tplDir := t.TempDir()
	tpl := makeTpl(t, tplDir, "t", &templates.Template{
		Files: []templates.File{{Path: "hello.md", Content: "world"}},
	})
	plan, err := Compute(nil, tpl, target, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("got %d items", len(plan.Items))
	}
	it := plan.Items[0]
	if it.Action != ActionCreate || it.Kind != KindFile {
		t.Errorf("unexpected: %+v", it)
	}
	if string(it.Content) != "world" {
		t.Errorf("content = %q", it.Content)
	}
}

func TestComputeSourceFile(t *testing.T) {
	target := t.TempDir()
	tplDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tplDir, "src.txt"), []byte("from-src"), 0o644); err != nil {
		t.Fatal(err)
	}
	tpl := makeTpl(t, tplDir, "t", &templates.Template{
		Files: []templates.File{{Path: "out.md", Source: "src.txt"}},
	})
	plan, err := Compute(nil, tpl, target, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if string(plan.Items[0].Content) != "from-src" {
		t.Errorf("content = %q", plan.Items[0].Content)
	}
}

func TestComputeIdenticalFileIsSilentNoOp(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "f"), []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	tplDir := t.TempDir()
	tpl := makeTpl(t, tplDir, "t", &templates.Template{
		Files: []templates.File{{Path: "f", Content: "same"}},
	})
	plan, err := Compute(nil, tpl, target, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 0 {
		t.Errorf("expected silent no-op; got %d", len(plan.Items))
	}
}

func TestComputeDifferentFileSkipsByDefault(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "f"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	tplDir := t.TempDir()
	tpl := makeTpl(t, tplDir, "t", &templates.Template{
		Files: []templates.File{{Path: "f", Content: "new"}},
	})
	plan, err := Compute(nil, tpl, target, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Action != ActionSkip {
		t.Errorf("plan = %+v", plan.Items)
	}
	if !strings.Contains(plan.Items[0].Reason, "force") {
		t.Errorf("reason missing --force hint: %q", plan.Items[0].Reason)
	}
}

func TestComputeForceOverwrites(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "f"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	tplDir := t.TempDir()
	tpl := makeTpl(t, tplDir, "t", &templates.Template{
		Files: []templates.File{{Path: "f", Content: "new"}},
	})
	plan, err := Compute(nil, tpl, target, Options{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Items[0].Action != ActionCreate {
		t.Errorf("Action = %s; want Create", plan.Items[0].Action)
	}
}

func TestComputeOnConflictOverwrite(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "f"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	tplDir := t.TempDir()
	tpl := makeTpl(t, tplDir, "t", &templates.Template{
		Files: []templates.File{{Path: "f", Content: "new", OnConflict: "overwrite"}},
	})
	plan, err := Compute(nil, tpl, target, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Items[0].Action != ActionCreate {
		t.Errorf("Action = %s; want Create", plan.Items[0].Action)
	}
}

func TestComputeOnConflictError(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "f"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	tplDir := t.TempDir()
	tpl := makeTpl(t, tplDir, "t", &templates.Template{
		Files: []templates.File{{Path: "f", Content: "new", OnConflict: "error"}},
	})
	if _, err := Compute(nil, tpl, target, Options{}); err == nil {
		t.Error("expected error from on_conflict: error policy")
	}
}

func TestComputeFileNeitherSourceNorContent(t *testing.T) {
	target := t.TempDir()
	tplDir := t.TempDir()
	tpl := makeTpl(t, tplDir, "t", &templates.Template{
		Files: []templates.File{{Path: "broken"}},
	})
	if _, err := Compute(nil, tpl, target, Options{}); err == nil {
		t.Error("expected error for file with neither source nor content")
	}
}

func TestComputeChildrenRecursion(t *testing.T) {
	target := t.TempDir()
	for _, sub := range []string{"alpha", "beta", "skip-me", ".dot"} {
		if err := os.MkdirAll(filepath.Join(target, "subs", sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	tplDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tplDir, "leaf.yaml"),
		[]byte("name: leaf\nfolders: [created]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	parent := makeTpl(t, tplDir, "parent", &templates.Template{
		Children: []templates.Child{
			{Parent: "subs", Template: "leaf", Exclude: []string{"skip-me"}},
		},
	})
	res := &templates.Resolver{SearchPath: []string{tplDir}}
	plan, err := Compute(res, parent, target, Options{})
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, it := range plan.Items {
		got = append(got, it.Abs)
	}
	sort.Strings(got)
	want := []string{
		filepath.Join(plan.Target, "subs", "alpha", "created"),
		filepath.Join(plan.Target, "subs", "beta", "created"),
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("recursed paths = %v; want %v", got, want)
	}
}

func TestComputeNoRecurse(t *testing.T) {
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "subs", "alpha"), 0o755); err != nil {
		t.Fatal(err)
	}
	tplDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tplDir, "leaf.yaml"),
		[]byte("name: leaf\nfolders: [created]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	parent := makeTpl(t, tplDir, "parent", &templates.Template{
		Children: []templates.Child{{Parent: "subs", Template: "leaf"}},
	})
	res := &templates.Resolver{SearchPath: []string{tplDir}}
	plan, err := Compute(res, parent, target, Options{NoRecurse: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 0 {
		t.Errorf("expected no items when NoRecurse=true; got %v", plan.Items)
	}
}

func TestComputeChildrenSkipsMissingParentDir(t *testing.T) {
	target := t.TempDir() // no `subs/` exists
	tplDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tplDir, "leaf.yaml"),
		[]byte("name: leaf\nfolders: [x]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	parent := makeTpl(t, tplDir, "parent", &templates.Template{
		Children: []templates.Child{{Parent: "subs", Template: "leaf"}},
	})
	res := &templates.Resolver{SearchPath: []string{tplDir}}
	plan, err := Compute(res, parent, target, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 0 {
		t.Errorf("missing parent should silently skip; got %v", plan.Items)
	}
}

func TestComputeDiffReportsExtras(t *testing.T) {
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "extra-folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "extra-file.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tplDir := t.TempDir()
	tpl := makeTpl(t, tplDir, "t", &templates.Template{
		Folders: []string{"declared"},
	})
	plan, err := Compute(nil, tpl, target, Options{Diff: true})
	if err != nil {
		t.Fatal(err)
	}
	var extras []string
	for _, it := range plan.Items {
		if it.Action == ActionExtra {
			extras = append(extras, it.Path)
		}
	}
	sort.Strings(extras)
	want := []string{"extra-file.md", "extra-folder"}
	if !reflect.DeepEqual(extras, want) {
		t.Errorf("extras = %v; want %v", extras, want)
	}
}

func TestComputeDiffSkipsContentUnderChildParent(t *testing.T) {
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "subs", "anything"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "subs", "anything", "f.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tplDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tplDir, "leaf.yaml"),
		[]byte("name: leaf\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	parent := makeTpl(t, tplDir, "parent", &templates.Template{
		Children: []templates.Child{{Parent: "subs", Template: "leaf"}},
	})
	res := &templates.Resolver{SearchPath: []string{tplDir}}
	plan, err := Compute(res, parent, target, Options{Diff: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range plan.Items {
		if it.Action == ActionExtra && strings.HasPrefix(it.Path, "subs"+string(filepath.Separator)) {
			t.Errorf("did not expect extra under child parent: %s", it.Path)
		}
	}
}

func TestApplyCreatesAndReportsSkipped(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "f"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	tplDir := t.TempDir()
	tpl := makeTpl(t, tplDir, "t", &templates.Template{
		Folders: []string{"newdir"},
		Files: []templates.File{
			{Path: "newfile", Content: "hello"},
			{Path: "f", Content: "new"}, // existing different — skip
		},
	})
	plan, err := Compute(nil, tpl, target, Options{})
	if err != nil {
		t.Fatal(err)
	}

	err = Apply(plan)
	var se *SkippedError
	if err == nil {
		t.Fatal("expected SkippedError")
	}
	if !errors.As(err, &se) {
		t.Fatalf("expected *SkippedError; got %T: %v", err, err)
	}
	if se.N != 1 {
		t.Errorf("Skipped.N = %d; want 1", se.N)
	}

	if _, err := os.Stat(filepath.Join(target, "newdir")); err != nil {
		t.Errorf("newdir missing: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(target, "newfile"))
	if err != nil || string(got) != "hello" {
		t.Errorf("newfile content = %q (err=%v)", got, err)
	}
	got, err = os.ReadFile(filepath.Join(target, "f"))
	if err != nil || string(got) != "old" {
		t.Errorf("existing file should be untouched; got %q (err=%v)", got, err)
	}
}

