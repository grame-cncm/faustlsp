package tests

import (
	"log/slog"
	"slices"
	"testing"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/parser"
	"github.com/carn181/faustlsp/server"
	"github.com/carn181/faustlsp/transport"
	"github.com/carn181/faustlsp/util"
)

func testParseImports(t *testing.T) {
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

func testParseASTNode(t *testing.T) {
	logging.Logger = slog.Default()
	parser.Init()
	code := `

import("test.dsp");
d = library("another.dsp");

a = 1;
b = 2;

c = i with { i = 2; j = 2;};

d = par(i, 4, c);

g = case{(x:y) => y:x; (x) => x;}
`
	tree := parser.ParseTree([]byte(code))
	defer tree.Close()

	root := tree.RootNode()

	s := server.Server{}
	s.Workspace = server.Workspace{}
	s.Workspace.Root = "./test-project"
	s.Workspace.Config = server.FaustProjectConfig{
		Command: "faustlsp",
	}

	file := server.File{
		Content: []byte(code),
		Handle:  util.FromPath("test.dsp"),
	}
	s.Workspace.ParseASTNode(root, &file, nil, nil, nil, nil)
}

func TestRangeContains(t *testing.T) {
	tests := []struct {
		name   string
		parent transport.Range
		child  transport.Range
		want   bool
	}{
		{
			name:   "Child has greater char range than parent",
			parent: transport.Range{Start: transport.Position{Line: 0, Character: 0}, End: transport.Position{Line: 2, Character: 0}},
			child:  transport.Range{Start: transport.Position{Line: 1, Character: 0}, End: transport.Position{Line: 1, Character: 17}},
			want:   true,
		},
		{
			name:   "Child has greater char range than parent",
			parent: transport.Range{Start: transport.Position{Line: 1, Character: 2}, End: transport.Position{Line: 1, Character: 13}},
			child:  transport.Range{Start: transport.Position{Line: 6, Character: 18}, End: transport.Position{Line: 6, Character: 19}},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := server.RangeContains(tt.parent, tt.child)
			if got != tt.want {
				t.Errorf("RangeContains() = %v, want %v", got, tt.want)
			}
		})
	}
}
