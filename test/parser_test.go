package tests

import (
	"slices"
	"testing"

	"github.com/carn181/faustlsp/parser"
)

func TestParseImports(t *testing.T) {
	parser.Init()
	code := []byte(`
import("a.lib");
import("s.dsp");
import("c.dsp");
`)
	tree := parser.ParseTree(code)
	rslts := parser.GetImports(code, tree)
	expected := []string{"a.lib", "s.dsp", "c.dsp"}
	if !slices.Equal(rslts, expected) {
		t.Error(parser.GetImports(code, tree))
	}
}
