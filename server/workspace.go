package server

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/util"

	"github.com/fsnotify/fsnotify"
	cp "github.com/otiai10/copy"
)

const faustConfigFile = ".faustcfg.json"

type WorkspaceFiles map[util.Path]*File

func (w WorkspaceFiles) LogValue() slog.Value {
	files := make([]any, 0, len(w))
	for key, value := range w {
		// Extract the File's LogValue to use its structured logging format
		fileValue := value.LogValue()
		// Since File.LogValue() now returns an AnyValue with a map,
		// we need to handle it differently
		files = append(files, map[string]any{
			"path": key,
			"file": fileValue.Any(),
		})
	}

	return slog.AnyValue(files)
}

type Workspace struct {
	// Path to Root Directory of Workspace
	Root     string
	Files    WorkspaceFiles
	mu       sync.Mutex
	TDEvents chan TDEvent
	config   FaustProjectConfig
}

func IsFaustFile(path util.Path) bool {
	ext := filepath.Ext(path)
	return ext == ".dsp" || ext == ".lib"
}

func IsDSPFile(path util.Path) bool {
	ext := filepath.Ext(path)
	return ext == ".dsp"
}

func IsLibFile(path util.Path) bool {
	ext := filepath.Ext(path)
	return ext == ".lib"
}

func (workspace *Workspace) Init(ctx context.Context, s *Server) {
	// Open all files in workspace and add to File Store
	workspace.Files = make(map[util.Path]*File)
	workspace.TDEvents = make(chan TDEvent)

	// Replicate Workspace in our Temp Dir by copying
	logging.Logger.Info("Current workspace root", "path", workspace.Root)
	folder := filepath.Base(workspace.Root)
	tempWorkspacePath := filepath.Join(s.tempDir, folder)
	err := cp.Copy(workspace.Root, tempWorkspacePath)
	if err != nil {
		logging.Logger.Error("Copying file error", "error", err)
	}
	logging.Logger.Info("Replicating Workspace in ", "path", tempWorkspacePath)

	// Parse Config File
	workspace.loadConfigFiles(s)

	// Open the files in file store
	err = filepath.Walk(workspace.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			_, ok := s.Files.Get(path)

			if !ok {
				// Path relative to workspace
				relPath := path[len(workspace.Root)+1:]
				workspaceFolderName := filepath.Base(workspace.Root)
				tempDirFilePath := filepath.Join(s.tempDir, workspaceFolderName, relPath)

				logging.Logger.Info("Opening file from workspace\n", "path", path)

				s.Files.OpenFromPath(path, workspace.Root, false, "", tempDirFilePath)
				workspace.addFileFromFileStore(path, s)

				// Create Import Tree
				// TODO: Implement updating this import tree
				workspace.CreateImportTree(&s.Files, relPath, workspace.Root)

				// Parse Symbols to Symbol Store
				file, _ := s.Files.Get(path)
				s.Files.mu.Lock()
				ParseSymbolsToStore(file, s)
				s.Files.mu.Unlock()
				workspace.DiagnoseFile(path, s)
			}
		}
		return nil
	})

	logging.Logger.Info("Workspace Files", "files", workspace.Files)
	logging.Logger.Info("File Store", "files", s.Files.String())

	go func() { workspace.StartTrackingChanges(ctx, s) }()
	logging.Logger.Info("Started workspace watcher\n")
}

func (workspace *Workspace) loadConfigFiles(s *Server) {
	f, ok := s.Files.Get(filepath.Join(workspace.Root, faustConfigFile))
	var cfg FaustProjectConfig
	var err error
	if ok {
		cfg, err = workspace.parseConfig(f.Content)
		if err != nil {
			cfg = workspace.defaultConfig()
		}
	} else {
		cfg = workspace.defaultConfig()
	}
	workspace.config = cfg
	logging.Logger.Info("Workspace Config", "config", cfg)
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
		logging.Logger.Error("Error in starting watcher", "error", err)
	}

	// Recursively add directories to watchlist
	err = filepath.Walk(workspace.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			watcher.Add(path)
			logging.Logger.Info("Watching file in workspace %s\n", path, workspace.Root)
		}
		return nil
	})

	for {
		select {
		// Editor TextDocument Events
		// Assumes Method Handler has handled this event and has this file in Files Store
		case change := <-workspace.TDEvents:
			logging.Logger.Info("Handling TD Event", "event", change)
			workspace.HandleEditorEvent(change, s)
		// Disk Events
		case event, ok := <-watcher.Events:
			logging.Logger.Info("Handling Workspace Disk Event", "event", event)
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
	origPath, err := filepath.Localize(event.Name)

	if err != nil {
		if runtime.GOOS == "windows" {
			logging.Logger.Error("Localizing error", "error", err)
		}
		origPath = event.Name
	}

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

	// Reload config file if changed
	if filepath.Base(relPath) == faustConfigFile {
		workspace.loadConfigFiles(s)
		workspace.cleanDiagnostics(s)
	}

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
				s.Files.OpenFromPath(origPath, s.Workspace.Root, false, "", tempDirFilePath)

				// Create File
				f, err := os.Create(tempDirFilePath)
				if err != nil {
					logging.Logger.Error("Create File error", "error", err)
				}
				f.Chmod(fi.Mode())
				f.Close()

				workspace.addFileFromFileStore(origPath, s)
			}
		} else {
			// Rename Create
			oldFileRelPath := event.RenamedFrom[len(workspace.Root)+1:]
			oldTempPath := filepath.Join(tempDir, workspaceFolderName, oldFileRelPath)

			if util.IsValidPath(tempDirFilePath) && util.IsValidPath(oldTempPath) {
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
		workspace.DiagnoseFile(origPath, s)
	}
}

func (workspace *Workspace) HandleEditorEvent(change TDEvent, s *Server) {
	// Temporary Directory
	tempDir := s.tempDir

	// Path of File that this Event affected
	origFilePath := change.Path

	// Reload config file if changed
	if filepath.Base(origFilePath) == faustConfigFile {
		workspace.loadConfigFiles(s)
		workspace.cleanDiagnostics(s)
	}

	file, ok := s.Files.Get(origFilePath)
	if !ok {
		logging.Logger.Error("File should've been in File Store.", "path", origFilePath)
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
				logging.Logger.Error("failed to create directory", "error", err)
				break
			}
		}

		// Create File in Temporary Directory. This creates an empty file at the temp path.
		f, err := os.Create(tempDirFilePath)
		if err != nil {
			logging.Logger.Error("OS create error", "error", err)
		}
		f.Close()
	case TDChange:
		// Write File to Temporary Directory. Updates the temporary file with the latest content from the editor.
		logging.Logger.Info("Writing recent change to", "path", tempDirFilePath)
		os.WriteFile(tempDirFilePath, file.Content, fs.FileMode(os.O_TRUNC)) // Write the file content to the temp file, overwriting existing content
		content, _ := os.ReadFile(tempDirFilePath)
		logging.Logger.Info("Current state of file", "path", tempDirFilePath, "content", string(content))
		workspace.DiagnoseFile(origFilePath, s)
	case TDClose:
		// Sync file from disk on close if it exists and replicate it to temporary directory, else remove from Files Store
		if util.IsValidPath(origFilePath) { // Check if the file path is valid
			s.Files.OpenFromPath(origFilePath, s.Workspace.Root, false, "", tempDirFilePath) // Reload the file from the specified path.

			file, ok := s.Files.Get(origFilePath) // Retrieve the file again (unnecessary, can use the previous `file`)
			if ok {
				os.WriteFile(tempDirFilePath, file.Content, os.FileMode(os.O_TRUNC)) // Write content to temporary file, replicating it from disk.
			}
			workspace.addFileFromFileStore(origFilePath, s)
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

func (w *Workspace) DiagnoseFile(path util.Path, s *Server) {
	if IsFaustFile(path) {
		logging.Logger.Info("Diagnosing File", "path", path)
		params := s.Files.TSDiagnostics(path)
		if params.URI != "" {
			s.diagChan <- params
		}
		if len(params.Diagnostics) == 0 {
			logging.Logger.Info("Generating Compiler errors as no syntax errors")
			// Compiler Diagnostics if exists
			if w.config.CompilerDiagnostics {
				w.sendCompilerDiagnostics(s)
			}
		}
	}
}

func (workspace *Workspace) removeFile(path util.Path) {
	workspace.mu.Lock()
	delete(workspace.Files, path)
	workspace.mu.Unlock()
}
