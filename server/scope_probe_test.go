package server

import (
	"context"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/parser"
	"github.com/carn181/faustlsp/transport"
	"github.com/carn181/faustlsp/util"
)

// The standard library's reverbs.lib parses to its library aliases, definitions, and functions.
func TestReverbsLibraryScope(t *testing.T) {
	logging.Init()
	parser.Init()
	var files Files
	files.Init(context.Background(), transport.UTF16)
	store := Store{Files: &files, Dependencies: NewDependencyGraph(), Cache: make(map[[sha256.Size]byte]*Scope)}
	ws := Workspace{Root: t.TempDir()}
	ws.Config.Command = "faust"
	path := util.Path("/opt/homebrew/share/faust/reverbs.lib")
	files.OpenFromPath(path)
	f, ok := files.GetFromPath(path)
	if !ok {
		t.Skip("reverbs.lib is not installed at the Homebrew path")
	}
	queue := []string{}
	ws.ParseFile(f, &store, map[util.Path]struct{}{}, &queue)
	names := []string{}
	for _, s := range FindSymbolsNew(f.Scope, "", &store, map[util.Path]struct{}{}) {
		names = append(names, s.name)
	}
	for _, want := range []string{"ma", "jcrev", "zita_rev1_stereo"} {
		if !slices.Contains(names, want) {
			t.Errorf("reverbs.lib symbols lack %q: %v", want, names)
		}
	}
}

// The cursor can sit one past the last byte of the content; the identifier before it is still found.
func TestIdentifierAtEndOfContent(t *testing.T) {
	content := []byte("import(\"stdfaust.lib\");\nre.")
	n := uint(len(content))
	ident, _ := FindSymbolScopeAtOffset(content, nil, n, "utf-16")
	if ident != "re." {
		t.Errorf("identifier at end of content = %q, want %q", ident, "re.")
	}
	pos, _ := OffsetToPosition(n, string(content), "utf-16")
	r := FindCompletionReplaceRange(pos, string(content), "utf-16")
	want := transport.Range{Start: transport.Position{Line: 1, Character: 3}, End: transport.Position{Line: 1, Character: 3}}
	if r != want {
		t.Errorf("replace range at end of content = %+v, want %+v", r, want)
	}
}

// Two analyses of a document that imports the standard library run one
// after the other without racing on the store or deadlocking on the
// library files' locks.
func TestConcurrentAnalysesOfAnImportingDocument(t *testing.T) {
	logging.Init()
	parser.Init()
	var files Files
	files.Init(context.Background(), transport.UTF32)
	store := Store{Files: &files, Dependencies: NewDependencyGraph(), Cache: make(map[[sha256.Size]byte]*Scope)}
	ws := Workspace{Root: t.TempDir()}
	ws.Config.Command = "faust"
	if _, err := exec.LookPath("faust"); err != nil {
		t.Skip("the faust command is not installed")
	}
	path := util.Path(filepath.Join(ws.Root, "program.dsp"))
	if err := os.WriteFile(path, []byte("import(\"stdfaust.lib\");\nprocess = fi.lowpass(2, 1000);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files.OpenFromPath(path)
	f, ok := files.GetFromPath(path)
	if !ok {
		t.Fatal("the document did not open")
	}
	done := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			ws.AnalyzeFile(f, &store)
			done <- struct{}{}
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Minute):
			t.Fatal("an analysis never finished")
		}
	}
	names := []string{}
	for _, s := range FindSymbolsNew(f.Scope, "", &store, map[util.Path]struct{}{}) {
		names = append(names, s.name)
	}
	if !slices.Contains(names, "fi") {
		t.Errorf("the document's scope lacks the library prefix fi: %v", names)
	}
}

// After a library prefix and part of a member's name, the completions are
// that library's members with that beginning, not the enclosing scope.
func TestMemberCompletionAfterPartOfAName(t *testing.T) {
	logging.Init()
	parser.Init()
	var files Files
	files.Init(context.Background(), transport.UTF32)
	store := Store{Files: &files, Dependencies: NewDependencyGraph(), Cache: make(map[[sha256.Size]byte]*Scope)}
	ws := Workspace{Root: t.TempDir()}
	ws.Config.Command = "faust"
	if _, err := exec.LookPath("faust"); err != nil {
		t.Skip("the faust command is not installed")
	}
	path := util.Path(filepath.Join(ws.Root, "program.dsp"))
	content := "import(\"stdfaust.lib\");\nprocess = _ : co.com"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	files.OpenFromPath(path)
	f, ok := files.GetFromPath(path)
	if !ok {
		t.Fatal("the document did not open")
	}
	ws.AnalyzeFile(f, &store)
	end := transport.Position{Line: 1, Character: uint32(len("process = _ : co.com"))}
	names := []string{}
	for _, s := range GetPossibleSymbols(end, path, &store, "utf-32") {
		names = append(names, s.name)
	}
	if !slices.Contains(names, "compressor_mono") {
		t.Errorf("co.com lacks compressor_mono: %v", names)
	}
	for _, name := range names {
		if !strings.HasPrefix(name, "com") {
			t.Errorf("co.com offers %q, which does not begin with com", name)
		}
	}
	// At the dot itself every member is offered.
	dot := transport.Position{Line: 1, Character: uint32(len("process = _ : co."))}
	all := GetPossibleSymbols(dot, path, &store, "utf-32")
	if len(all) <= len(names) {
		t.Errorf("co. offers %d members, co.com %d", len(all), len(names))
	}
}

// A definition whose comment is separated from it by `declare` lines still
// has that comment as its documentation.
func TestDocumentationPastDeclareLines(t *testing.T) {
	logging.Init()
	parser.Init()
	var files Files
	files.Init(context.Background(), transport.UTF16)
	store := Store{Files: &files, Dependencies: NewDependencyGraph(), Cache: make(map[[sha256.Size]byte]*Scope)}
	ws := Workspace{Root: t.TempDir()}
	ws.Config.Command = "faust"
	path := util.Path("/opt/homebrew/share/faust/compressors.lib")
	files.OpenFromPath(path)
	f, ok := files.GetFromPath(path)
	if !ok {
		t.Skip("compressors.lib is not installed at the Homebrew path")
	}
	queue := []string{}
	ws.ParseFile(f, &store, map[util.Path]struct{}{}, &queue)
	for _, s := range FindSymbolsNew(f.Scope, "", &store, map[util.Path]struct{}{}) {
		if s.name == "compressor_stereo" {
			if !strings.Contains(s.docs.Full, "(co.)compressor_stereo") {
				t.Errorf("compressor_stereo documentation: %q", s.docs.Full)
			}
			return
		}
	}
	t.Error("compressors.lib symbols lack compressor_stereo")
}

// A doc block begins at its dashed header; the section banner above it is
// not documentation.
func TestDocumentationStartsAtTheHeader(t *testing.T) {
	logging.Init()
	parser.Init()
	var files Files
	files.Init(context.Background(), transport.UTF16)
	store := Store{Files: &files, Dependencies: NewDependencyGraph(), Cache: make(map[[sha256.Size]byte]*Scope)}
	ws := Workspace{Root: t.TempDir()}
	ws.Config.Command = "faust"
	path := util.Path("/opt/homebrew/share/faust/misceffects.lib")
	files.OpenFromPath(path)
	f, ok := files.GetFromPath(path)
	if !ok {
		t.Skip("misceffects.lib is not installed at the Homebrew path")
	}
	queue := []string{}
	ws.ParseFile(f, &store, map[util.Path]struct{}{}, &queue)
	for _, s := range FindSymbolsNew(f.Scope, "", &store, map[util.Path]struct{}{}) {
		if s.name == "cubicnl" {
			if !strings.HasPrefix(strings.TrimSpace(s.docs.Full), "---") || strings.Contains(s.docs.Full, "====") {
				t.Errorf("cubicnl documentation: %q", s.docs.Full[:min(120, len(s.docs.Full))])
			}
			return
		}
	}
	t.Error("misceffects.lib symbols lack cubicnl")
}
