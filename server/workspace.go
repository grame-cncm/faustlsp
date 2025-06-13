package server

import "sync"

type Workspace struct {
	// Path to Root Directory of Workspace
	Root  string
	Files map[string]*File
	mu    sync.Mutex
}
