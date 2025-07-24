package server

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/util"

	"github.com/fsnotify/fsnotify"
	cp "github.com/otiai10/copy"
)

const faustConfigFile = ".faustcfg.json"

type WorkspaceFiles []util.Path

func (w WorkspaceFiles) LogValue() slog.Value {
	files := make([]any, 0, len(w))
	for _, key := range w {
		files = append(files, map[string]any{
			"path": key,
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
	Config   FaustProjectConfig

	// Temporary directory where this workspace is replicated
	tempDir     util.Path
	openedFiles map[util.Handle]struct{}
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

func (workspace *Workspace) TempDirPath(filePath util.Path) util.Path {
	result := filepath.Join(workspace.tempDir, filePath)
	return result
}

func (workspace *Workspace) Init(ctx context.Context, s *Server) {
	// Open all files in workspace and add to File Store
	workspace.Files = []util.Path{}
	workspace.TDEvents = make(chan TDEvent)
	workspace.openedFiles = make(map[util.Handle]struct{})
	workspace.tempDir = s.tempDir

	// Replicate Workspace in our Temp Dir by copying
	logging.Logger.Info("Current workspace root", "path", workspace.Root)

	tempWorkspacePath := filepath.Join(s.tempDir, workspace.Root)
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
			f, ok := s.Files.GetFromPath(path)

			if !ok {
				// Path relative to workspace
				logging.Logger.Info("Opening file from workspace\n", "path", path)

				s.Files.OpenFromPath(path)

				workspace.addFile(path)

				f, _ = s.Files.GetFromPath(path)

				workspace.DiagnoseFile(path, s)
			}
			workspace.AnalyzeFile(f, &s.Store)
		}
		return nil
	})

	logging.Logger.Info("Workspace Files", "files", workspace.Files)
	logging.Logger.Info("File Store", "files", &s.Files)

	go func() { workspace.StartTrackingChanges(ctx, s) }()
	logging.Logger.Info("Started workspace watcher\n")
}

func (workspace *Workspace) loadConfigFiles(s *Server) {
	configFilePath := filepath.Join(workspace.Root, faustConfigFile)
	f, ok := s.Files.GetFromPath(configFilePath)
	var cfg FaustProjectConfig
	var err error
	if ok {
		f.mu.RLock()
		cfg, err = workspace.parseConfig(f.Content)
		f.mu.RUnlock()
		if err != nil {
			cfg = workspace.defaultConfig()
		}
	} else {
		// Try opening file if not opened but it exists
		s.Files.OpenFromPath(configFilePath)
		f, ok := s.Files.GetFromPath(configFilePath)
		if ok {
			f.mu.RLock()
			cfg, err = workspace.parseConfig(f.Content)
			f.mu.RUnlock()
			if err != nil {
				cfg = workspace.defaultConfig()
			}
		} else {
			cfg = workspace.defaultConfig()
		}
	}
	workspace.Config = cfg
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
	watcher.Add(workspace.Root)
	err = filepath.Walk(workspace.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			watcher.Add(path)
			logging.Logger.Info("Adding directory to watcher\n", path, workspace.Root)
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

	// If file of this path is already opened by editor, ignore this HandleDiskEvent
	_, open := workspace.openedFiles[util.FromPath(origPath)]
	if open {
		return
	}

	// Path relative to workspace
	relPath := origPath[len(workspace.Root)+1:]

	// Reload config file if changed
	if filepath.Base(relPath) == faustConfigFile {
		workspace.loadConfigFiles(s)
		workspace.cleanDiagnostics(s)
	}

	// The equivalent of the workspace file path for the temporary directory
	// Should be of the form TEMP_DIR/WORKSPACE_ROOT_PATH/relPath
	tempDirFilePath := workspace.TempDirPath(origPath)
	logging.Logger.Info("Got disk event for file", "path", origPath, "temp", tempDirFilePath, "event", event)

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
				s.Files.OpenFromPath(origPath)

				// Create File
				f, err := os.Create(tempDirFilePath)
				if err != nil {
					logging.Logger.Error("Create File error", "error", err)
				}
				f.Chmod(fi.Mode())
				f.Close()

				workspace.addFile(origPath)
			}
		} else {
			// Rename Create
			oldFileRelPath := event.RenamedFrom[len(workspace.Root)+1:]
			oldTempPath := filepath.Join(workspace.tempDir, workspace.Root, oldFileRelPath)

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
		s.Files.RemoveFromPath(origPath)
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

	file, ok := s.Files.GetFromPath(origFilePath)
	if !ok {
		logging.Logger.Error("File should've been in File Store.", "path", origFilePath)
	}

	tempDirFilePath := filepath.Join(tempDir, origFilePath) // Construct the temporary file path
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
			s.Files.OpenFromPath(origFilePath) // Reload the file from the specified path.

			file, ok := s.Files.GetFromPath(origFilePath) // Retrieve the file again (unnecessary, can use the previous `file`)
			if ok {
				os.WriteFile(tempDirFilePath, file.Content, os.FileMode(os.O_TRUNC)) // Write content to temporary file, replicating it from disk.
			}
			workspace.addFile(origFilePath)
		} else {
			s.Files.RemoveFromPath(origFilePath) // Remove the file from the file store if the path isn't valid
		}

	}
}

func (workspace *Workspace) EditorOpenFile(uri util.URI, files *Files) {
	files.OpenFromURI(uri)
	handle, _ := util.FromURI(uri)
	workspace.openedFiles[handle] = struct{}{}
}

func (workspace *Workspace) addFile(path util.Path) {
	workspace.mu.Lock()
	workspace.Files = append(workspace.Files, path)
	workspace.mu.Unlock()
}

func (w *Workspace) DiagnoseFile(path util.Path, s *Server) {
	if IsFaustFile(path) {
		logging.Logger.Info("Diagnosing File", "path", path)
		params := s.Files.TSDiagnostics(path)
		logging.Logger.Info("Got Diagnose File", "params", params)
		if params.URI != "" {
			s.diagChan <- params
		}
		if len(params.Diagnostics) == 0 {
			// Compiler Diagnostics if exists
			if w.Config.CompilerDiagnostics {
				logging.Logger.Info("Generating Compiler errors as no syntax errors")
				w.sendCompilerDiagnostics(s)
			}
		}
	}
}

func (workspace *Workspace) removeFile(path util.Path) {
	workspace.mu.Lock()
	for i, filePath := range workspace.Files {
		if filePath == path {
			workspace.Files = slices.Delete(workspace.Files, i, i)
		}
	}
	workspace.mu.Unlock()
}
