package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/parser"
	"github.com/carn181/faustlsp/transport"
	"github.com/carn181/faustlsp/util"

	//	"strings"
	"sync"
	"unicode/utf8"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type File struct {
	URI      util.Uri
	Path     util.Path
	RelPath  util.Path // Path relative to a workspace
	TempPath util.Path // Path for temporary
	Content  []byte
	Open     bool
	Tree     *tree_sitter.Tree
	// To avoid freeing null tree in C
	treeCreated     bool
	hasSyntaxErrors bool
}

// Concurrency issues with Treesitter Tree
// 1) Concurrently trying to do things with f.Tree, impossible
// 2) Doing something with Tree while concurrently it is closed and reopened => Therefore needs to be copied for these operations

func (f *File) DocumentSymbols() []transport.DocumentSymbol {
	t := f.Tree.Clone()
	defer t.Close()
	return parser.DocumentSymbols(t, f.Content)
	//	return []transport.DocumentSymbol{}
}

func (f *File) TSDiagnostics() transport.PublishDiagnosticsParams {
	t := f.Tree.Clone()
	defer t.Close()
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

func (files *Files) OpenFromURI(uri util.Uri, root util.Path, editorOpen bool, temp util.Path) {
	path, err := util.Uri2path(uri)
	if err != nil {
		logging.Logger.Println(err)
		return
	}
	files.OpenFromPath(path, root, editorOpen, uri, temp)
}

func (files *Files) OpenFromPath(path util.Path, root util.Path, editorOpen bool, uri util.Uri, temp util.Path) {
	var file File

	var relPath util.Path
	_, ok := files.Get(path)
	// If File already in store, ignore
	if ok {
		//		logging.Logger.Printf("File already in store with contents: %s\n", f.Content)
		return
	}

	if root == "" {
		relPath = ""
	} else {
		size := len(root)
		// +1 for / delimeter for only relative path
		relPath = path[size+1:]
	}
	logging.Logger.Printf("Reading contents of file %s\n", path)

	content, err := os.ReadFile(path)

	if err != nil {
		if os.IsNotExist(err) {
			logging.Logger.Println("Invalid Path " + err.Error())
		}
	}

	// Parse Tree
	var tree *tree_sitter.Tree
	var treemade bool
	ext := filepath.Ext(path)
	if ext == ".dsp" || ext == ".lib" {
		//		logging.Logger.Printf("Trying to parse %s\n", content)
		tree = parser.ParseTree(content)
		treemade = true
	} else {
		treemade = false
	}

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
		logging.Logger.Printf("error: file to modify not in file store (%s)\n", path)
		return
	}

	f.Content = []byte(content)

	ext := filepath.Ext(path)
	if ext == ".dsp" || ext == ".lib" {
		if f.treeCreated {
			f.Tree.Close()
		}
		//		logging.Logger.Printf("Trying to parse %s\n", f.Content)
		f.Tree = parser.ParseTree(f.Content)
		f.treeCreated = true
	}
	files.mu.Unlock()
}

// TODO: Implement ModifyIncremental
func (files *Files) ModifyIncremental(path util.Path, changeRange transport.Range, content string) {
	logging.Logger.Printf("Applying Incremental Change to %s\n", path)
	files.mu.Lock()
	f, ok := files.fs[path]
	if !ok {
		logging.Logger.Printf("error: file to modify not in file store (%s)\n", path)
		return
	}
	result := ApplyIncrementalChange(changeRange, content, string(f.Content), string(files.encoding))
	//	logging.Logger.Printf("Before:\n%s\nAfter:\n%s\n", string(f.Content), result)
	f.Content = []byte(result)

	ext := filepath.Ext(path)
	if ext == ".dsp" || ext == ".lib" {
		if f.treeCreated {
			f.Tree.Close()
		}
		//		logging.Logger.Printf("Trying to parse %s\n", f.Content)
		f.Tree = parser.ParseTree(f.Content)
		f.treeCreated = true
	}
	files.mu.Unlock()
}

// TODO: Maybe have the 3 following functions in util instead of here
func ApplyIncrementalChange(r transport.Range, newContent string, content string, encoding string) string {
	start, _ := PositionToOffset(r.Start, content, encoding)
	end, _ := PositionToOffset(r.End, content, encoding)
	//	logging.Logger.Printf("Start: %d, End: %d\n", start, end)
	return content[:start] + newContent + content[end:]
}

func PositionToOffset(pos transport.Position, s string, encoding string) (uint, error) {
	if len(s) == 0 {
		return 0, nil
	}
	indices := GetLineIndices(s)
	if pos.Line > uint32(len(indices)) {
		return 0, fmt.Errorf("Invalid Line Number")
	} else if pos.Line == uint32(len(indices)) {
		return uint(len(s)), nil
	}
	//	logging.Logger.Print(indices)
	currChar := indices[pos.Line]
	//	logging.Logger.Printf("Line Start: %d\n", currChar)
	for i := 0; i < int(pos.Character); i++ {
		r, w := utf8.DecodeRuneInString(s[currChar:])
		currChar += uint(w)
		//		fmt.Printf("Curr Char rn: %d\n",currChar)

		// If protocol is sending utf-16 offset, increment current character index if it has surrogate code-point
		if encoding == "utf-16" {
			//			logging.Logger.Println("Got UTF-16 Encoding")
			if r >= 0x10000 {
				i++
				if i == int(pos.Character) {
					break
				}
			}
			// Some clients like emacs lsp-mode do not properly support utf-16
		} else if encoding == "utf-32" {
			//			logging.Logger.Println("Got UTF-32 Encoding")
		}
	}
	return currChar, nil
}

func GetLineIndices(s string) []uint {
	//	logging.Logger.Printf("Got %s\n", s)
	lines := []uint{0}
	i := 0
	for w := 0; i < len(s); i += w {
		runeValue, width := utf8.DecodeRuneInString(s[i:])
		if runeValue == '\n' {
			lines = append(lines, uint(i)+1)
		}
		w = width
	}
	return lines
}

func (files *Files) CloseFromURI(uri util.Uri) {
	path, err := util.Uri2path(uri)
	if err != nil {
		logging.Logger.Println(err)
		return
	}
	files.Close(path)
}

func (files *Files) Close(path util.Path) {
	files.mu.Lock()
	f, ok := files.fs[path]
	if !ok {
		logging.Logger.Printf("error: file to close not in file store (%s)\n", path)
		return
	}
	f.Open = false
	files.mu.Unlock()
}

func (files *Files) Remove(path util.Path) {
	files.mu.Lock()
	// TODO: Have a close function for File
	f, ok := files.fs[path]
	if ok {
		ext := filepath.Ext(path)
		if ext == ".dsp" || ext == ".lib" {
			if f.treeCreated {
				f.Tree.Close()
			}
		}
	}
	delete(files.fs, path)
	files.mu.Unlock()
}

func (files *Files) String() string {
	str := ""
	for path, f := range files.fs {
		str += fmt.Sprintf("%s\n %s\n", path, string(f.Content))
	}
	return str
}
