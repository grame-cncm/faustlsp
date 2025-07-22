package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"sync"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/parser"
	"github.com/carn181/faustlsp/transport"
	"github.com/carn181/faustlsp/util"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type File struct {
	// Ensure thread-safety for modifications
	mu sync.RWMutex

	URI  util.URI
	Path util.Path

	// TODO: Shift to Handle instead of URI and Path
	handle util.Handle

	RelPath  util.Path // Path relative to a workspace
	TempPath util.Path // Path for temporary

	Syms    []*IdentifierSym
	Imports []*File

	Content []byte

	Open bool
	Tree *tree_sitter.Tree
	// To avoid freeing null tree in C
	treeCreated     bool
	hasSyntaxErrors bool
}

func (f *File) LogValue() slog.Value {
	// Create a map with all file attributes
	var imports = []util.Path{}
	for _, imported := range f.Imports {
		imports = append(imports, imported.Path)
	}
	fileAttrs := map[string]any{
		"URI":      f.URI,
		"Path":     f.Path,
		"RelPath":  f.RelPath,
		"TempPath": f.TempPath,
		"Imports":  imports,
	}
	return slog.AnyValue(fileAttrs)
}
func (f *File) DocumentSymbols() []transport.DocumentSymbol {
	f.mu.RLock()
	defer f.mu.RUnlock()

	t := parser.ParseTree(f.Content)
	defer t.Close()
	return parser.DocumentSymbols(t, f.Content)
	//	return []transport.DocumentSymbol{}
}

func (f *File) TSDiagnostics() transport.PublishDiagnosticsParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := parser.ParseTree(f.Content)
	//	defer t.Close()
	errors := parser.TSDiagnostics(f.Content, t)
	if len(errors) == 0 {
		f.hasSyntaxErrors = false
	} else {
		f.hasSyntaxErrors = true
	}
	d := transport.PublishDiagnosticsParams{
		URI:         transport.DocumentURI(f.URI),
		Diagnostics: errors,
	}
	return d
}

type Files struct {
	// Absolute Paths Only
	fs       map[util.Path]*File
	mu       sync.Mutex
	encoding transport.PositionEncodingKind // Position Encoding for applying incremental changes. UTF-16 and UTF-32 supported
}

func (files *Files) Init(context context.Context, encoding transport.PositionEncodingKind) {
	files.fs = make(map[string]*File)
	files.encoding = encoding
}

func (files *Files) OpenFromURI(uri util.URI, root util.Path, editorOpen bool, temp util.Path) {
	path, err := util.URI2path(uri)
	if err != nil {
		logging.Logger.Error("OpenFromURI error", "error", err)
		return
	}
	files.OpenFromPath(path, root, editorOpen, uri, temp)
}

func (files *Files) OpenFromPath(path util.Path, root util.Path, editorOpen bool, uri util.URI, temp util.Path) {
	var file File

	var relPath util.Path
	_, ok := files.Get(path)
	// If File already in store, ignore
	if ok {
		logging.Logger.Info("File already in store", "path", path)
		return
	}

	if root == "" {
		relPath = ""
	} else {
		size := len(root)
		// +1 for / delimeter for only relative path
		relPath = path[size+1:]
	}
	logging.Logger.Info("Reading contents of file", "path", path)

	content, err := os.ReadFile(path)

	if err != nil {
		if os.IsNotExist(err) {
			logging.Logger.Error("Invalid Path", "error", err)
			return
		}
	}

	// Parse Tree
	var tree *tree_sitter.Tree
	var treemade bool

	if uri == "" {
		uri = util.Path2URI(path)
	}
	file = File{
		Path:        path,
		Content:     content,
		Open:        editorOpen,
		RelPath:     relPath,
		Tree:        tree,
		TempPath:    temp,
		treeCreated: treemade,
		URI:         uri,
	}

	files.mu.Lock()
	files.fs[path] = &file
	files.mu.Unlock()
}

func (files *Files) Get(path util.Path) (*File, bool) {
	files.mu.Lock()
	file, ok := files.fs[path]
	files.mu.Unlock()
	return file, ok
}

func (files *Files) TSDiagnostics(path util.Path) transport.PublishDiagnosticsParams {
	d := transport.PublishDiagnosticsParams{}
	files.mu.Lock()
	file, ok := files.fs[path]
	if ok {
		d = file.TSDiagnostics()

	}
	files.mu.Unlock()
	return d
}

func (files *Files) ModifyFull(path util.Path, content string) {
	files.mu.Lock()
	f, ok := files.fs[path]
	if !ok {
		logging.Logger.Error("file to modify not in file store", "path", path)
		files.mu.Unlock()
		return
	}

	f.mu.Lock()
	f.Content = []byte(content)
	f.mu.Unlock()

	files.mu.Unlock()
}

func (files *Files) ModifyIncremental(path util.Path, changeRange transport.Range, content string) {
	logging.Logger.Info("Applying Incremental Change", "path", path)
	files.mu.Lock()
	f, ok := files.fs[path]
	if !ok {
		logging.Logger.Error("file to modify not in file store", "path", path)
		files.mu.Unlock()
		return
	}
	result := ApplyIncrementalChange(changeRange, content, string(f.Content), string(files.encoding))
	//	logging.Logger.Info("Before/After Incremental Change", "before", string(f.Content), "after", result)
	logging.Logger.Info("Incremental Change Parameters ", "range", changeRange, "content", content)
	logging.Logger.Info("Before/After Incremental Change", "before", string(f.Content), "after", result)

	f.mu.Lock()
	f.Content = []byte(result)
	f.mu.Unlock()

	files.mu.Unlock()
}

func (files *Files) CloseFromURI(uri util.URI) {
	path, err := util.URI2path(uri)
	if err != nil {
		logging.Logger.Error("CloseFromURI error", "error", err)
		return
	}
	files.Close(path)
}

func (files *Files) Close(path util.Path) {
	files.mu.Lock()
	f, ok := files.fs[path]
	if !ok {
		logging.Logger.Error("file to close not in file store", "path", path)
		files.mu.Unlock()
		return
	}
	f.mu.Lock()
	f.Open = false
	f.mu.Unlock()
	files.mu.Unlock()
}

func (files *Files) Remove(path util.Path) {
	files.mu.Lock()
	delete(files.fs, path)
	files.mu.Unlock()
}

func (files *Files) String() string {
	str := ""
	for path := range files.fs {
		if IsFaustFile(path) {
			str += fmt.Sprintf("Files has %s\n", path)
		}
	}
	return str
}
