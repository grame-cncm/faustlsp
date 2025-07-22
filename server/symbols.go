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

type Identifier = string

type SymbolStore struct {
	mu sync.Mutex
	// Map from symbol to all possible symbols for the given symbol
	syms map[Identifier][]*IdentifierSym
}

func (s *SymbolStore) Init() {
	s.syms = make(map[Identifier][]*IdentifierSym)
}

type IdentifierSym struct {
	Name       string
	Definition transport.Location
	Uses       []*File // Function Calls/Use
}

func IsImported(f File, target util.Handle) bool {
	files := f.Imports
	for _, file := range files {
		fileHandle := util.FromPath(file.Path)
		if fileHandle == target {
			return true
		}
		IsImported(f, fileHandle)
	}
	return false
}

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

	f.mu.RLock()
	fileContent := f.Content
	f.mu.RUnlock()

	tree := parser.ParseTree(fileContent)
	defer tree.Close()
	imports := parser.GetImports(fileContent, tree)
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
			f.mu.Lock()
			f.Imports = append(f.Imports, importFile)
			f.mu.Unlock()
			continue

		}

		w.CreateImportTree(fs, importPath, resolvedRootDir)

		//		fs.mu.Lock()
		importFile, ok = fs.Get(resolvePath)
		if !ok {
			logging.Logger.Error("imported File should've been in store if it exists", "importing file", file.Path, "imported file path", resolvePath)
		}
		fs.mu.Lock()
		f.mu.Lock()
		f.Imports = append(f.Imports, importFile)
		f.mu.Unlock()
		fs.mu.Unlock()
		//		fs.mu.Unlock()
	}
}

func ParseSymbolsToStore(file *File, s *Server) {
	// Need a symbolstore and TSParser
	// Avoid parsing visited files

	visitedFiles := make(map[util.Path]struct{})

	var parseSymbols func(f *File)
	parseSymbols = func(f *File) {

		// Parse this file
		queryStr := `
(program
(function_definition name: (identifier) @definition)
(definition variable: (identifier) @definition))
`

		f.mu.RLock()
		fileContent := f.Content
		f.mu.RUnlock()
		tree := parser.ParseTree(fileContent)
		rslts := parser.GetQueryMatches(queryStr, fileContent, tree)
		defer tree.Close()

		logging.Logger.Info("Got rslts for current file", "path", f.Path)

		storeSymbols(rslts, f, &s.Symbols)

		// Parse the imports
		f.mu.RLock()
		imports := f.Imports
		f.mu.RUnlock()

		for _, importFile := range imports {
			_, ok := visitedFiles[importFile.Path]
			if !ok {
				visitedFiles[importFile.Path] = struct{}{}
				parseSymbols(importFile)
			}
		}
	}

	parseSymbols(file)

	//	logging.Logger.Info("Parsed Symbols as", "symbols", s.Symbols.syms)

}

func storeSymbols(rslts parser.TSQueryResult, file *File, symbols *SymbolStore) {

	for symbolType, nodes := range rslts.Results {
		// TODO: Use symbolType later for handling library statements
		_ = symbolType
		for _, node := range nodes {
			//			logging.Logger.Info("Got Result", "type", symbolType, "node", node.Utf8Text(content))

			file.mu.Lock()
			identName := node.Utf8Text(file.Content)
			start := node.StartPosition()
			end := node.EndPosition()

			var identifier = IdentifierSym{
				Name: identName,
				Definition: transport.Location{
					URI: transport.DocumentURI(file.URI),
					Range: transport.Range{
						Start: transport.Position{
							Line:      uint32(start.Row),
							Character: uint32(start.Column),
						},
						End: transport.Position{
							Line:      uint32(end.Row),
							Character: uint32(end.Column),
						},
					},
				},
			}
			file.Syms = append(file.Syms, &identifier)
			file.mu.Unlock()

			symbols.mu.Lock()
			matches, ok := symbols.syms[identName]
			if ok {
				symbols.syms[identName] = append(matches, &identifier)
			} else {
				symbols.syms[identName] = []*IdentifierSym{&identifier}
			}
			symbols.mu.Unlock()
		}
	}
}
