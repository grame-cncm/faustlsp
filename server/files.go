package server

import (
	"faustlsp/logging"
	"faustlsp/util"
	"fmt"
	"io/fs"
	"os"
	"sync"
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
	_, ok := files.fs[path]
	// If File already in store, ignore
	if ok {
		return
	}

	if root == "" {
		relPath = ""
	} else {
		size := len(root)
		// +1 for / delimeter for only relative path
		relPath = path[size+1:]
	}

	if fs.ValidPath(path) {
		content, err := os.ReadFile(path)
		if err != nil {
			logging.Logger.Println(err)
		}
		file = File{
			Path:    path,
			Content: content,
			Open:    editorOpen,
			RelPath: relPath,
		}
	} else {
		file = File{
			Path:    path,
			Content: []byte{},
			Open:    editorOpen,
			RelPath: relPath,
		}
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
func (files *Files) ModifyIncremental(path util.Path, content string) {
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
