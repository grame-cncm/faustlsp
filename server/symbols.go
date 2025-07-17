package server

import (
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/parser"
	"github.com/carn181/faustlsp/transport"
	"github.com/carn181/faustlsp/util"
)

// 1) Symbol map(Symbol->Symbols with similar name parsed in project)
//        |-> Useful for code completion identifiers, hover,  goto def and ref,
//  2)  Maybe have a file store with pointers to symbols defined in it along with pointers to other files
//    Useful to have a knowledge of what symbols are in scope for a file
// 3) Import resolve to a particular file store pointer
// 4) All this should be computed right after TD/Change is applied
// 5) Need to figure out how to evaluate this partially, rather than computing the whole symbol store every TDChange

type SymbolStore struct {
	mu   sync.Mutex
	syms map[util.Handle][]*IdentifierSym
}

type SymbolFilesStore struct {
	mu    sync.Mutex
	files map[util.Handle][]*SymbolsFile
}

func (store *SymbolStore) Add(handle util.Handle, sym *IdentifierSym) {
	store.mu.Lock()
	syms := store.syms[handle]
	store.syms[handle] = append(syms, sym)
	store.mu.Unlock()
}

type IdentifierSym struct {
	Name       string
	Definition transport.Location
}

type SymbolsFile struct {
	Handle        util.Handle
	Syms          []*IdentifierSym
	ImportedFiles []*SymbolsFile
}

func IsImported(f SymbolsFile, target util.Handle) bool {
	files := f.ImportedFiles
	for _, file := range files {
		if file.Handle == target {
			return true
		}
		IsImported(f, file.Handle)
	}
	return false
}

// On workspace creation
// 1) Go through all files
// 2) First add all dependency trees to SymbolFilesStore. If file already imported, only get pointer from that
// 3) Then parse all top-level symbols defined in each file and store. Same logic as above
// 4) Test and see current store state when given a file

// TODO: Need some way to delete from these stores too
// TODO: Handle library expressions

// SymbolFilesStoreAddFilesBasic(File) {
// imports := GetImports(File)
// SymbolsFile{Handle := File.Handle}
// for _, import := range imports {
// SymbolFilesStoreAddFilesBasic
//    SymbolsFile.ImportedFiles = append(..., resolve(import))
// }
//   sf.files[File.Handle] := &SymbolsFile
// }

func (w *Workspace) GetFaustDSPDir() string {
	faustCommand := w.config.Command
	_, err := exec.LookPath(faustCommand)
	if err != nil {
		logging.Logger.Error("Couldn't find faust command in PATH", "cmd", faustCommand)
	}
	var output strings.Builder
	cmd := exec.Command(faustCommand, "-dspdir")
	cmd.Stdout = &output

	_ = cmd.Run()
	faustDSPDirPath := output.String()
	// Remove \n at the end
	faustDSPDirPath = faustDSPDirPath[:len(faustDSPDirPath)-1]
	return faustDSPDirPath
}

// Resolves a given file path like the Faust compiler does when it has to import a file
func (w *Workspace) ResolveFilePath(relPath util.Path, rootDir util.Path) (util.Path, util.Path) {
	// File in workspace
	path1 := filepath.Join(rootDir, relPath)
	logging.Logger.Info("Trying path", "path", path1)
	if util.IsValidPath(path1) {
		return path1, rootDir
	}

	// File in Faust System Library DSP directory
	faustDSPDir := w.GetFaustDSPDir()
	path2 := filepath.Join(faustDSPDir, relPath)
	logging.Logger.Info("Trying path", "path", path2)
	if util.IsValidPath(path2) {
		return path2, faustDSPDir
	}

	return "", ""
}

// Traverses through imports of file and adds them to Store along with import information
func (w *Workspace) CreateImportTree(fs *Files, fileRelPath util.Path, rootDir util.Path) {
	// TODO: Take relative paths and rootDir, instead of util.Handle for versatility for -dspdir or include paths
	fullPath := filepath.Join(rootDir, fileRelPath)
	file := util.FromPath(fullPath)

	f, ok := fs.Get(file.Path)
	if !ok {
		//		logging.Logger.Error("File not in store", "path", file.Path)

		resolvedFilePath, resolvedRootDir := w.ResolveFilePath(fileRelPath, rootDir)
		if resolvedFilePath == "" {
			logging.Logger.Error("Couldn't find file", "path", fullPath)
			return
		}

		resolvedFile := util.FromPath(resolvedFilePath)

		// TODO: Create temporary files for resolved import files as well
		//       E.g. TEMP/faustlsp/faust
		// TODO: Use ID's along with temp-dir paths to avoid name clashes
		fs.OpenFromPath(resolvedFilePath, resolvedRootDir, true, resolvedFile.URI, resolvedFile.Path)
		f, ok = fs.Get(resolvedFilePath)
	}
	//	logging.Logger.Info("Getting Imports for file", "path", f.Path)

	tree := parser.ParseTree(f.Content)
	defer tree.Close()
	imports := parser.GetImports(f.Content, tree)
	//	logging.Logger.Info("Got imports for file", "path", f.Path, "imports", imports)
	for _, importPath := range imports {

		resolvePath, resolvedRootDir := w.ResolveFilePath(importPath, rootDir)

		if resolvePath == f.Path {
			//			logging.Logger.Info("Trying to import same file, avoiding cycle", "path", resolvePath)
			continue
		}

		importFile, ok := fs.Get(resolvePath)
		if ok {
			//			logging.Logger.Info("Imported file already added in store, skipping", "path", resolvePath)
			f.Imports = append(f.Imports, importFile)
			continue

		}

		w.CreateImportTree(fs, importPath, resolvedRootDir)

		//		fs.mu.Lock()
		importFile, ok = fs.Get(resolvePath)
		if !ok {
			logging.Logger.Error("imported File should've been in store if it exists", "importing file", file.Path, "imported file path", resolvePath)
		}
		fs.mu.Lock()
		f.Imports = append(f.Imports, importFile)
		fs.mu.Unlock()
		//		fs.mu.Unlock()
	}
}
