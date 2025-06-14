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
	logging.Logger.Printf("File Store: %s\n", s.Files.String())

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

	for {
		select {
		// Editor TextDocument Events
		// Assumes Method Handler has handled this event and has this file in Files Store
		case change := <-workspace.TDEvents:
			logging.Logger.Printf("Handling TD Event: %v\n", change)
			workspace.HandleEditorEvent(change, s)
		// Disk Events
		case event, ok := <-watcher.Events:
			logging.Logger.Printf("Handling Workspace Disk Event: %s\n", event)
			if !ok {
				return
			}
			workspace.HandleDiskEvent(event, s, watcher)
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

func (workspace *Workspace) HandleDiskEvent(event fsnotify.Event, s *Server, watcher *fsnotify.Watcher) {
	// Path of original file
	origPath := event.Name

	// Temporary Directory to use
	tempDir := s.tempDir

	// If file of this path is already open in File Store, ignore this event
	file, ok := s.Files.Get(origPath)
	if ok {
		if file.Open {
			return
		}
	}

	// Path relative to workspace
	relPath := origPath[len(workspace.Root)+1:]
	// Workspace Folder name
	workspaceFolderName := filepath.Base(workspace.Root)

	// The equivalent of the workspace file path for the temporary directory
	// Should be of the form TEMP_DIR/WORKSPACE_FOLDER_NAME/relPath
	tempDirFilePath := filepath.Join(tempDir, workspaceFolderName, relPath)

	// OS CREATE Event
	if event.Has(fsnotify.Create) {
		// Check if this is a rename Create or a normal new file create. fsnotify sends a rename and create event on file renames and the create event has the RenamedFrom field
		if event.RenamedFrom == "" {
			// Normal New File
			// Ensure path exists to copy
			// Sometimes files get deleted by text editors before this goroutine can handle it
			fi, err := os.Stat(origPath)
			if err != nil {
				return
			}

			if fi.IsDir() {
				// If a directory is being created, mkdir instead of create
				os.MkdirAll(tempDirFilePath, fi.Mode().Perm())
				// Add this new directory to watch as watcher does not recursively watch subdirectories
				watcher.Add(origPath)
			} else {
				// Add it our server tracking and workspace
				s.Files.OpenFromPath(origPath, s.Workspace.Root, false)
				workspace.addFileFromFileStore(origPath, s)

				// Create File
				f, err := os.Create(tempDirFilePath)
				if err != nil {
					logging.Logger.Fatal(err)
				}
				f.Chmod(fi.Mode())
				f.Close()
			}
		} else {
			// Rename Create
			oldFileRelPath := event.RenamedFrom[len(workspace.Root)+1:]
			oldTempPath := filepath.Join(tempDir, workspaceFolderName, oldFileRelPath)

			if fs.ValidPath(tempDirFilePath) && fs.ValidPath(oldTempPath) {
				err := os.Rename(oldTempPath, tempDirFilePath)
				if err != nil {
					return
				}
			}

			fi, _ := os.Stat(origPath)
			if fi.IsDir() {
				// Add this new directory to watch as watcher does not recursively watch subdirectories
				watcher.Add(origPath)
			}
		}
	}

	// OS REMOVE Event
	if event.Has(fsnotify.Remove) {
		// Remove from File Store, Workspace and Temp Directory
		s.Files.Remove(origPath)
		workspace.removeFile(origPath)
		os.Remove(tempDirFilePath)
	}

	// OS WRITE Event
	if event.Has(fsnotify.Write) {
		contents, _ := os.ReadFile(origPath)
		os.WriteFile(tempDirFilePath, contents, fs.FileMode(os.O_TRUNC))
		s.Files.ModifyFull(origPath, string(contents))
	}
}

func (workspace *Workspace) HandleEditorEvent(change TDEvent, s *Server) {
	// Temporary Directory
	tempDir := s.tempDir

	// Path of File that this Event affected
	origFilePath := change.Path

	file, ok := s.Files.Get(origFilePath)
	if !ok {
		logging.Logger.Fatalf("File %s should've been in File Store.", origFilePath)
	}

	workspaceFolderName := filepath.Base(workspace.Root)
	tempDirFilePath := filepath.Join(tempDir, workspaceFolderName, file.RelPath) // Construct the temporary file path
	switch change.Type {
	case TDOpen:
		// Ensure directory exists before creating file. This mirrors the workspace's directory structure in the temp directory.
		dirPath := filepath.Dir(tempDirFilePath)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			err := os.MkdirAll(dirPath, 0755) // Create the directory and all parent directories with permissions 0755
			if err != nil {
				logging.Logger.Fatalf("failed to create directory: %s", err)
				break
			}
		}

		// Create File in Temporary Directory. This creates an empty file at the temp path.
		f, err := os.Create(tempDirFilePath)
		if err != nil {
			logging.Logger.Fatal(err)
		}
		f.Close()
	case TDChange:
		// Write File to Temporary Directory. Updates the temporary file with the latest content from the editor.
		logging.Logger.Printf("Writing recent change to %s\n", tempDirFilePath)
		os.WriteFile(tempDirFilePath, file.Content, fs.FileMode(os.O_TRUNC)) // Write the file content to the temp file, overwriting existing content
	case TDClose:
		// Sync file from disk on close if it exists and replicate it to temporary directory, else remove from Files Store
		if fs.ValidPath(origFilePath) { // Check if the file path is valid
			s.Files.OpenFromPath(origFilePath, s.Workspace.Root, false) // Reload the file from the specified path.
			workspace.addFileFromFileStore(origFilePath, s)

			file, ok := s.Files.Get(origFilePath) // Retrieve the file again (unnecessary, can use the previous `file`)
			if ok {
				os.WriteFile(tempDirFilePath, file.Content, os.FileMode(os.O_TRUNC)) // Write content to temporary file, replicating it from disk.
			}
		} else {
			s.Files.Remove(origFilePath) // Remove the file from the file store if the path isn't valid
		}

	}
}

func (workspace *Workspace) addFileFromFileStore(path util.Path, s *Server) {
	file, _ := s.Files.Get(path)
	workspace.mu.Lock()
	workspace.Files[path] = file
	workspace.mu.Unlock()
}

func (workspace *Workspace) removeFile(path util.Path) {
	workspace.mu.Lock()
	delete(workspace.Files, path)
	workspace.mu.Unlock()
}
