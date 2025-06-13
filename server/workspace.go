package server

import (
	"context"
	"faustlsp/logging"
	"faustlsp/util"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	cp "github.com/otiai10/copy"
)

type Workspace struct {
	// Path to Root Directory of Workspace
	Root     string
	Files    map[util.Path]*File
	mu       sync.Mutex
	TDEvents chan TDEvent
}

func (workspace *Workspace) Init(ctx context.Context, s *Server) {
	// Open all files in workspace and add to File Store
	workspace.Files = make(map[util.Path]*File)
	workspace.TDEvents = make(chan TDEvent)
	err := filepath.Walk(workspace.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			_, ok := s.Files.Get(path)
			if !ok {
				//				logging.Logger.Printf("Opening file from workspace: %s\n", path)
				s.Files.OpenFromPath(path, workspace.Root, false)
				file, _ := s.Files.Get(path)
				workspace.Files[path] = file
			}
		}
		return nil
	})

	logging.Logger.Printf("Workspace Files: %v\n", workspace.Files)
	logging.Logger.Printf("File Store: %\n", s.Files)

	// Replicate Workspace in our Temp Dir by copying
	folder := filepath.Base(workspace.Root)
	tempWorkspacePath := filepath.Join(s.tempDir, folder)
	err = cp.Copy(workspace.Root, tempWorkspacePath)
	logging.Logger.Printf("Replicated Workspace in %s\n", tempWorkspacePath)

	go workspace.StartTrackingChanges(ctx, s)

	if err != nil {
		logging.Logger.Fatal(err)
	}
}

// Track and Replicate Changes to workspace
// TODO: Refactor and simplify
// TODO: Avoid repetition of getting relative paths
func (workspace *Workspace) StartTrackingChanges(ctx context.Context, s *Server) {
	// 1) Open All Files in Path with absolute Path recursively, store in s.Files, give pointers to Workspace.Files
	// 2) Copy Directory to TempDir Workspace
	// 3) Start Watching Changes like util
	//    3*) If File open, get changes from filebuffer
	//    3**) Replicate in disk + replicate in memory all these changes in both Files and Workspace.files

	// Ideal Pipeline
	// File Paths -> Content{Get from disk, Get from text document changes} -> Replicate in Disk TempDir -> ParseSymbols/Get Diagnostics from TempDir and Memory
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logging.Logger.Fatal(err)
	}

	// Recursively add directories to watchlist
	err = filepath.Walk(workspace.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			watcher.Add(path)
			logging.Logger.Printf("Watching %s in workspace %s\n", path, workspace.Root)
		}
		return nil
	})

	replicdir := s.tempDir

	for {
		select {
		// Editor TextDocument Events
		// Assumes Method Handler as handled this event and has this file in Files Store
		case change := <-workspace.TDEvents:
			logging.Logger.Printf("Handling TD Event: %d\n", change)
			file, ok := s.Files.Get(change.Path)
			if !ok {
				logging.Logger.Fatalf("File %s should've been in File Store.", change.Path)
			}

			workspaceFolderName := filepath.Base(workspace.Root)
			temp_path := filepath.Join(replicdir, workspaceFolderName, file.RelPath)
			switch change.Type {
			case TDOpen:
				// Ensure directory exists before creating file
				dirPath := filepath.Dir(temp_path)
				if _, err := os.Stat(dirPath); os.IsNotExist(err) {
					err := os.MkdirAll(dirPath, 0755)
					if err != nil {
						logging.Logger.Fatalf("failed to create directory: %s", err)
						break
					}
				}

				// Create File in Temporary Directory
				f, err := os.Create(temp_path)
				if err != nil {
					logging.Logger.Fatal(err)
				}
				f.Close()
			case TDChange:
				// Write File to Temporary Directory
				logging.Logger.Printf("Writing recent change to %s\n", temp_path)
				os.WriteFile(temp_path, file.Content, fs.FileMode(os.O_TRUNC))
			case TDClose:
				// Sync file from disk on close if it exists and replicate it to temporary directory, else remove from Files Store
				if fs.ValidPath(change.Path) {
					s.Files.OpenFromPath(change.Path, s.Workspace.Root, false)
					file, _ := s.Files.Get(change.Path)
					workspace.Files[change.Path] = file
					file, ok := s.Files.Get(change.Path)
					if ok {
						os.WriteFile(temp_path, file.Content, os.FileMode(os.O_TRUNC))
					}
				} else {
					s.Files.Remove(change.Path)
				}

			}
		// Disk Events
		case event, ok := <-watcher.Events:
			logging.Logger.Printf("Handling Workspace Disk Event: %s\n", event)
			if !ok {
				return
			}
			// Path of original file
			path := event.Name
			// Path to replicate file
			rel_path := path[len(workspace.Root)+1:]
			workspaceFolderName := filepath.Base(workspace.Root)
			temp_path := filepath.Join(replicdir, workspaceFolderName, rel_path)

			// If file of this path is already open in File Store, ignore this event
			file, ok := s.Files.Get(path)
			if ok {
				if file.Open {
					break
				}
			}

			// OS CREATE Event
			if event.Has(fsnotify.Create) {
				if event.RenamedFrom == "" {
					// Normal New File
					// Ensure path exists to copy
					// Sometimes files get deleted by text editors before this goroutine can handle it
					fi, err := os.Stat(path)
					if err != nil {
						break
					}

					// If it is a directory, mkdir
					if fi.IsDir() {
						os.MkdirAll(temp_path, fi.Mode().Perm())
						// Add this new directory to watch as watcher does not recursively watch subdirectories
						watcher.Add(path)
					} else {
						// Add it our server tracking and workspace
						s.Files.OpenFromPath(path, s.Workspace.Root, false)
						file, _ := s.Files.Get(path)
						workspace.Files[path] = file

						f, err := os.Create(temp_path)
						if err != nil {
							panic(err)
						}
						f.Chmod(fi.Mode())
						f.Close()
					}

				} else {
					// Rename Create
					rel_path := event.RenamedFrom[len(workspace.Root)+1:]
					old_temp_path := filepath.Join(replicdir, workspaceFolderName, rel_path)
					if fs.ValidPath(temp_path) && fs.ValidPath(old_temp_path) {
						err := os.Rename(old_temp_path, temp_path)
						if err != nil {
							break
						}
					}
					fi, _ := os.Stat(path)
					if fi.IsDir() {
						// Add this new directory to watch as watcher does not recursively watch subdirectories
						watcher.Add(path)
					}
				}
			}

			// OS REMOVE Event
			if event.Has(fsnotify.Remove) {
				s.Files.Remove(path)
				delete(workspace.Files, path)
				os.Remove(temp_path)
			}

			// OS WRITE Event
			if event.Has(fsnotify.Write) {
				contents, _ := os.ReadFile(path)
				os.WriteFile(temp_path, contents, fs.FileMode(os.O_TRUNC))
				s.Files.ModifyFull(path, string(contents))
			}
		// Watcher Errors
		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
		// Cancel from parent
		case <-ctx.Done():
			watcher.Close()
			return
		}
	}
}
