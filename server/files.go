package server

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"

	"sync"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/parser"
	"github.com/carn181/faustlsp/transport"
	"github.com/carn181/faustlsp/util"
)

type File struct {
	// Ensure thread-safety for modifications
	mu     sync.RWMutex
	Handle util.Handle

	// A file's Syntax Tree Scope. Contains all symbols that are accessible in it.
	// Parent of this scope will be nil
	Scope *Scope

	// File Content
	Content []byte

	// Hash for each file. Used for caching scopes.
	Hash [sha256.Size]byte

	// TODO: Shift away from using this in diagnostics checking step
	hasSyntaxErrors bool
}

func (f *File) LogValue() slog.Value {
	// Create a map with all file attributes
	fileAttrs := map[string]any{
		"Handle": f.Handle,
		"Hash":   f.Hash,
		"Scope":  f.Scope,
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
	logging.Logger.Info("Waiting for lock", "file", f.Handle.Path)
	f.mu.Lock()

	logging.Logger.Info("Got lock", "file", f.Handle.Path)
	t := parser.ParseTree(f.Content)

	errors := parser.TSDiagnostics(f.Content, t)
	if len(errors) == 0 {
		f.hasSyntaxErrors = false
	} else {
		f.hasSyntaxErrors = true
	}
	d := transport.PublishDiagnosticsParams{
		URI:         transport.DocumentURI(f.Handle.URI),
		Diagnostics: errors,
	}
	f.mu.Unlock()
	return d
}

type Files struct {
	// Absolute Paths Only
	fs       map[util.Handle]*File
	mu       sync.Mutex
	encoding transport.PositionEncodingKind // Position Encoding for applying incremental changes. UTF-16 and UTF-32 supported
}

func (files *Files) Init(context context.Context, encoding transport.PositionEncodingKind) {
	files.fs = make(map[util.Handle]*File)
	files.encoding = encoding
}

func (files *Files) OpenFromURI(uri util.URI) {
	handle, err := util.FromURI(uri)
	if err != nil {
		logging.Logger.Error("Invalid URI", "uri", uri, "error", err)
	}
	files.Open(handle)
}

func (files *Files) OpenFromPath(path util.Path) {
	handle := util.FromPath(path)
	files.Open(handle)
}

func (files *Files) Open(handle util.Handle) {
	_, ok := files.Get(handle)
	// If File already in store, ignore
	if ok {
		logging.Logger.Info("File already in store", "handle.Path", handle.Path)
		return
	}
	logging.Logger.Info("Reading contents of file", "handle.Path", handle.Path)

	content, err := os.ReadFile(handle.Path)

	if err != nil {
		if os.IsNotExist(err) {
			logging.Logger.Error("Invalid Path", "error", err)
			return
		}
	}

	var file = File{
		Handle:  handle,
		Content: content,
		Hash:    sha256.Sum256(content),
	}

	files.mu.Lock()
	files.fs[handle] = &file
	files.mu.Unlock()
}

func (files *Files) AddFromURI(uri util.URI, content []byte) {
	handle, err := util.FromURI(uri)
	if err != nil {
		return
	}
	files.Add(handle, content)
}

func (files *Files) Add(handle util.Handle, content []byte) {
	var file = File{
		Handle: handle, Content: content, Hash: sha256.Sum256(content),
	}
	files.mu.Lock()
	files.fs[handle] = &file
	files.mu.Unlock()
}

func (files *Files) Get(handle util.Handle) (*File, bool) {
	files.mu.Lock()
	file, ok := files.fs[handle]
	files.mu.Unlock()
	return file, ok
}

func (files *Files) GetFromPath(path util.Path) (*File, bool) {
	handle := util.FromPath(path)
	file, ok := files.Get(handle)
	return file, ok
}

func (files *Files) GetFromURI(uri util.URI) (*File, bool) {
	handle, err := util.FromURI(uri)
	if err != nil {
		return nil, false
	}
	file, ok := files.Get(handle)
	return file, ok
}

func (files *Files) TSDiagnostics(path util.Path) transport.PublishDiagnosticsParams {
	d := transport.PublishDiagnosticsParams{}

	file, ok := files.GetFromPath(path)
	files.mu.Lock()
	if ok {
		d = file.TSDiagnostics()

	}
	files.mu.Unlock()
	return d
}

func (files *Files) ModifyFull(path util.Path, content string) {

	f, ok := files.GetFromPath(path)
	if !ok {
		logging.Logger.Error("file to modify not in file store", "path", path)
		files.mu.Unlock()
		return
	}

	files.mu.Lock()
	f.mu.Lock()
	f.Content = []byte(content)
	f.Hash = sha256.Sum256(f.Content)
	f.mu.Unlock()

	files.mu.Unlock()
}

func (files *Files) ModifyIncremental(path util.Path, changeRange transport.Range, content string) {
	logging.Logger.Info("Applying Incremental Change", "path", path)

	f, ok := files.GetFromPath(path)
	if !ok {
		logging.Logger.Error("file to modify not in file store", "path", path)
		files.mu.Unlock()
		return
	}
	result := ApplyIncrementalChange(changeRange, content, string(f.Content), string(files.encoding))
	//	logging.Logger.Info("Before/After Incremental Change", "before", string(f.Content), "after", result)
	logging.Logger.Info("Incremental Change Parameters ", "range", changeRange, "content", content)
	logging.Logger.Info("Before/After Incremental Change", "before", string(f.Content), "after", result)

	files.mu.Lock()
	f.mu.Lock()
	f.Content = []byte(result)
	f.Hash = sha256.Sum256(f.Content)
	f.mu.Unlock()

	files.mu.Unlock()
}

func (files *Files) CloseFromURI(uri util.URI) {
	handle, err := util.FromURI(uri)
	if err != nil {
		logging.Logger.Error("CloseFromURI error", "error", err)
		return
	}
	files.Close(handle)
}

func (files *Files) CloseFromPath(path util.Path) {
	handle := util.FromPath(path)
	files.Close(handle)
}

func (files *Files) Close(handle util.Handle) {
	files.mu.Lock()
	f, ok := files.fs[handle]
	if !ok {
		logging.Logger.Error("file to close not in file store", "handle", handle)
		files.mu.Unlock()
		return
	}
	f.mu.Lock()
	f.mu.Unlock()
	files.mu.Unlock()
}

func (files *Files) RemoveFromPath(path util.Path) {
	handle := util.FromPath(path)
	files.mu.Lock()
	delete(files.fs, handle)
	files.mu.Unlock()
}

func (files *Files) RemoveFromURI(uri util.URI) {
	handle, _ := util.FromURI(uri)
	files.mu.Lock()
	delete(files.fs, handle)
	files.mu.Unlock()
}

func (files *Files) Remove(handle util.Handle) {
	files.mu.Lock()
	delete(files.fs, handle)
	files.mu.Unlock()
}

func (files *Files) String() string {
	str := ""
	for handle := range files.fs {
		if IsFaustFile(handle.Path) {
			str += fmt.Sprintf("Files has %s\n", handle)
		}
	}
	return str
}

func (files *Files) LogValue() slog.Value {
	fs := make([]any, 0, len(files.fs))
	files.mu.Lock()
	defer files.mu.Unlock()

	for handle, file := range files.fs {
		if IsFaustFile(handle.Path) {
			// Use each file's LogValue method to get its proper representation
			fileValue := file.LogValue()
			fs = append(fs, fileValue.Any())
		}
	}
	return slog.AnyValue(fs)
}
