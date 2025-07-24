package tests

import (
	"log/slog"
	"slices"
	"testing"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/parser"
	"github.com/carn181/faustlsp/server"
	"github.com/carn181/faustlsp/util"
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

func TestParseASTNode(t *testing.T) {
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
	s.Workspace.ParseASTNode(root, &file, nil, nil, nil)
}
