package templates

import (
	"reflect"
	"testing"
)

func TestExpand(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"no braces", "a/b", []string{"a/b"}},
		{"empty string", "", []string{""}},
		{"plain literal", "plain", []string{"plain"}},
		{"two-way infix", "a/{b,c}", []string{"a/b", "a/c"}},
		{"three-way infix", "pre/{x,y,z}/suf", []string{"pre/x/suf", "pre/y/suf", "pre/z/suf"}},
		{"cartesian", "{a,b}/{x,y}", []string{"a/x", "a/y", "b/x", "b/y"}},
		{"nested", "{a,{b,c}/d}", []string{"a", "b/d", "c/d"}},
		{"empty branch preserved", "a/{}/b", []string{"a//b"}},
		{"single branch no comma", "{a}", []string{"a"}},
		{"unclosed brace literal", "{a,b", []string{"{a,b"}},
		{"trailing comma yields empty branch", "{a,}", []string{"a", ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Expand(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Expand(%q) = %v; want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestExpandMany(t *testing.T) {
	in := []string{"a", "{x,y}/z", "plain"}
	want := []string{"a", "x/z", "y/z", "plain"}
	got := ExpandMany(in)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExpandMany(%v) = %v; want %v", in, got, want)
	}
}

func TestExpandManyEmpty(t *testing.T) {
	got := ExpandMany(nil)
	if len(got) != 0 {
		t.Errorf("ExpandMany(nil) = %v; want empty", got)
	}
}
