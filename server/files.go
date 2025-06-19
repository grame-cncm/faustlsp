package server

import (
	"faustlsp/logging"
	"faustlsp/transport"
	"faustlsp/util"
	"fmt"
	"os"

	//	"strings"
	"sync"
	"unicode/utf8"
)

type File struct {
	Path    util.Path
	RelPath util.Path // Path relative to a workspace
	Content []byte
	Open    bool
}

type Files struct {
	// Absolute Paths Only
	fs map[util.Path]*File
	mu sync.Mutex
}

func (files *Files) Init() {
	files.fs = make(map[string]*File)
}

func (files *Files) OpenFromURI(uri util.Uri, root util.Path, editorOpen bool) {
	path, err := util.Uri2path(uri)
	if err != nil {
		logging.Logger.Println(err)
		return
	}
	files.OpenFromPath(path, root, editorOpen)
}

func (files *Files) OpenFromPath(path util.Path, root util.Path, editorOpen bool) {
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
		if os.IsNotExist(err){
			logging.Logger.Println("Invalid Path "+err.Error())
		}
	}

	file = File{
		Path:    path,
		Content: content,
		Open:    editorOpen,
		RelPath: relPath,
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

func (files *Files) ModifyFull(path util.Path, content string) {
	files.mu.Lock()
	f, ok := files.fs[path]
	if !ok {
		logging.Logger.Printf("error: file to modify not in file store (%s)\n", path)
		return
	}
	f.Content = []byte(content)
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
	result := ApplyIncrementalChange(changeRange, content, string(f.Content))
	//	logging.Logger.Printf("Before:\n%s\nAfter:\n%s\n", string(f.Content), result)
	f.Content = []byte(result)
	files.mu.Unlock()
}

func ApplyIncrementalChange(r transport.Range, newContent string, content string) string {
	start, _ := PositionToOffset(r.Start, content)
	end, _ := PositionToOffset(r.End, content)
	//	logging.Logger.Printf("Start: %d, End: %d\n", start, end)
	return content[:start] + newContent + content[end:]
}

func PositionToOffset(pos transport.Position, s string) (uint, error) {
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
		_, w := utf8.DecodeRuneInString(s[currChar:])
		currChar += uint(w)
		//		fmt.Printf("Curr Char rn: %d\n",currChar)
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
