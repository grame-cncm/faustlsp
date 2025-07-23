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
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
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

	// Used to distinguish between symbols in a single file
	// Empty for top-level definitions
	// Variable name for environments
	// Expression for with and letrec environments
	// Needs function that gets scopename for any node
	ScopeName string
}

type EnvironmentSym struct {
	Name       string
	Definition transport.Location
	Symbols    []*IdentifierSym

	ScopeName string
}

// Model using scopes + symbols
// Let's say we start with a.dsp
// Scope all top-levels in a.dsp
// All environments in a.dsp as Scope with type
// Let's say it imports another file
// Scope of that file is converted to this file and our scope symbols adds all those
// If it's a library, assign scope to the variable like environments

type SymbolKind int

const (
	Definition SymbolKind = iota
	Environment
	Library
)

type Symbol interface {
	Name() string
	Kind() int
	Location() transport.Location
	SetLocation(transport.Location)

	Scope() *Scope
	SetScope(*Scope)
}

type Scope struct {
	Parent  *Scope
	Symbols map[string]Symbol
}

// BaseSymbol provides common fields for embedding in concrete symbol types.
type BaseSymbol struct {
	N   string
	K   SymbolKind
	Loc Location
	Scp *Scope
}

type Location struct {
	File     util.Handle
	Position transport.Range
}

func (b *BaseSymbol) Name() string             { return b.N }
func (b *BaseSymbol) Kind() SymbolKind         { return b.K }
func (b *BaseSymbol) Location() Location       { return b.Loc }
func (b *BaseSymbol) SetLocation(loc Location) { b.Loc = loc }
func (b *BaseSymbol) Scope() *Scope            { return b.Scp }
func (b *BaseSymbol) SetScope(s *Scope)        { b.Scp = s }

type IdentifierSymbol struct {
	BaseSymbol
}

type EnvironmentSymbol struct {
	BaseSymbol
	Scope *Scope
}

func NewScope(parent *Scope) *Scope {
	return &Scope{
		Parent:  parent,
		Symbols: make(map[string]Symbol),
	}
}

// DependencyGraph manages the import relationships between files.
type DependencyGraph struct {
	mu sync.RWMutex // Protects concurrent access

	// Adjacency list: maps an importer's Path to a set of Paths it imports.
	// We use map[string]struct{} for a set.
	imports map[string]map[string]struct{}

	// Reverse adjacency list: maps an imported Path to a set of Paths that import it.
	// This is crucial for efficiently finding "dependents" when a file changes.
	importedBy map[string]map[string]struct{}

	// Tracks files currently being analyzed/processed to detect cycles.
	// Maps file Path to true if it's currently in the analysis stack.
	processing map[string]bool
}

// AddDependency records that 'importerURI' imports 'importedURI'.
func (dg *DependencyGraph) AddDependency(importerPath, importedPath util.Path) {
	dg.mu.Lock()
	defer dg.mu.Unlock()

	if _, ok := dg.imports[importerPath]; !ok {
		dg.imports[importerPath] = make(map[string]struct{})
	}
	dg.imports[importerPath][importedPath] = struct{}{}

	if _, ok := dg.importedBy[importedPath]; !ok {
		dg.importedBy[importedPath] = make(map[string]struct{})
	}
	dg.importedBy[importedPath][importerPath] = struct{}{}
}

// Call this before re-analyzing a file, as its imports might have changed.
func (dg *DependencyGraph) RemoveDependenciesForFile(path util.Path) {
	dg.mu.Lock()
	defer dg.mu.Unlock()

	// Remove its outgoing dependencies
	if importedURIs, ok := dg.imports[path]; ok {
		for importedURI := range importedURIs {
			delete(dg.importedBy[importedURI], path) // Remove self from imported's list
			if len(dg.importedBy[importedURI]) == 0 {
				delete(dg.importedBy, importedURI) // Clean up empty sets
			}
		}
		delete(dg.imports, path) // Remove its own entry
	}

	// Remove any incoming dependencies (if another file was importing it)
	// This is effectively handled by the other file being re-analyzed or removed.
	// But good to clean up if the file itself is deleted.
	delete(dg.importedBy, path) // If this file was being imported
}

// GetImporters returns a list of URIs that import the given file.
func (dg *DependencyGraph) GetImporters(uri string) []string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	importers := []string{}
	if s, ok := dg.importedBy[uri]; ok {
		for importerURI := range s {
			importers = append(importers, importerURI)
		}
	}
	return importers
}

// For a given file:
// 1) Create scope
// 2) Go through AST
// 3) Add definitions to scope
// 4) If import, first recurse through that file and then add all the symbols in that file's scope to this
// 5) If library, same but do as environment instead of global scope
// 6)
// END) Set file scope to this scope

// First parse all scopes with all imports and library
// When you get goto definition, keep going up in parent scopes till you find a matching symbol
// When you get goto references, first find definition, then go down in scopes and find uses of that variable (EXPENSIVE)
// Maybe compute a reference table for this too. Only problem is mapping

// Every document change, parse out these scopes visiting the whole import tree (EXPENSIVE)
// Solution: Add hashes to file based on content. Keep cached storage of scopes for a certain hash. Recompute when a child has a different hash.
// Also handle unresolved symbols in all these cases. You might get gibberish in the AST from tree-sitter

// Code Completion: Get all symbols in scope + primitives -> Fuzzy match with node in cursor

// 1) Files Store with scope associated with each file
// 2) Reference Lookup Map for a given defined symbol. Key = struct{file, name, line, char}
// 3) Dependency Graph of Files to know which files to update

// To update a file
//

// A given file's scope depends on the files it imports
// So when a file updates, if it doesn't have imports, just parse as normal and modify our file store file scope
// If it has imports, before parsing our file, go through our imported files. Keep going till leaf file and check if it is cached. Then use cached result, and go up. Keep doing.

type SymbolKey struct {
	File util.Path
	Name string
	Line uint
	Char uint
}

type ReferenceMap struct {
	references map[SymbolKey][]Symbol
}

type Store struct {
	mu           sync.Mutex
	Files        Files
	References   ReferenceMap
	Dependencies DependencyGraph
}

func ParseFileAndAddToStore(f *File, s *Server) {
	f.mu.RLock()
	f.Scope = NewScope(nil)
	// Parse through AST from f.Content
	tree := parser.ParseTree(f.Content)
	defer tree.Close()
	root := tree.RootNode()

	var traverseAST func(node *tree_sitter.Node, scope *Scope)
	traverseAST = func(node *tree_sitter.Node, scope *Scope) {
		nodeName := node.GrammarName()

		// If function_definition or definition, add it to current scope
		if nodeName == "definition" || nodeName == "function_definition" {
			definitionName := node.Child(0)
			if definitionName != nil {
				expression := node.ChildByFieldName("value")
				exprType := expression.GrammarName()

				switch exprType {
				case "with_environment", "letrec_environment", "environment":
					// TODO: Envionment symbol kind with scope
					traverseAST(expression, NewScope(scope))
				case "library":
				}
			}
		} else {
			for i := uint(0); i < node.ChildCount(); i++ {
				n := node.Child(i)
				traverseAST(n, scope)
			}
		}

		// Go to expression and recurse if it is a with/letrec environment or environment and start new scope
	}
	traverseAST(root, f.Scope)
	f.mu.RUnlock()
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

// // Traverses through imports of file and adds them to Store along with import information
// func (w *Workspace) CreateImportTree(fs *Files, fileRelPath util.Path, rootDir util.Path) {
//	// TODO: Take relative paths and rootDir, instead of util.Handle for versatility for -dspdir or include paths
//	fullPath := filepath.Join(rootDir, fileRelPath)
//	file := util.FromPath(fullPath)

//	f, ok := fs.Get(file.Path)
//	if !ok {
//		//		logging.Logger.Error("File not in store", "path", file.Path)

//		resolvedFilePath, resolvedRootDir := w.ResolveFilePath(fileRelPath, rootDir)
//		if resolvedFilePath == "" {
//			logging.Logger.Error("Couldn't find file", "path", fullPath)
//			return
//		}

//		resolvedFile := util.FromPath(resolvedFilePath)

//		// TODO: Create temporary files for resolved import files as well
//		//       E.g. TEMP/faustlsp/faust
//		// TODO: Use ID's along with temp-dir paths to avoid name clashes
//		fs.OpenFromPath(resolvedFilePath, resolvedRootDir, true, resolvedFile.URI, resolvedFile.Path)
//		f, ok = fs.Get(resolvedFilePath)
//	}
//	//	logging.Logger.Info("Getting Imports for file", "path", f.Path)

//	f.mu.RLock()
//	fileContent := f.Content
//	f.mu.RUnlock()

//	tree := parser.ParseTree(fileContent)
//	defer tree.Close()
//	imports := parser.GetImports(fileContent, tree)
//	//	logging.Logger.Info("Got imports for file", "path", f.Path, "imports", imports)
//	for _, importPath := range imports {

//		resolvePath, resolvedRootDir := w.ResolveFilePath(importPath, rootDir)

//		if resolvePath == f.Path {
//			//			logging.Logger.Info("Trying to import same file, avoiding cycle", "path", resolvePath)
//			continue
//		}

//		importFile, ok := fs.Get(resolvePath)
//		if ok {
//			//			logging.Logger.Info("Imported file already added in store, skipping", "path", resolvePath)
//			f.mu.Lock()
//			f.Imports = append(f.Imports, importFile)
//			f.mu.Unlock()
//			continue

//		}

//		w.CreateImportTree(fs, importPath, resolvedRootDir)

//		//		fs.mu.Lock()
//		importFile, ok = fs.Get(resolvePath)
//		if !ok {
//			logging.Logger.Error("imported File should've been in store if it exists", "importing file", file.Path, "imported file path", resolvePath)
//		}
//		fs.mu.Lock()
//		f.mu.Lock()
//		f.Imports = append(f.Imports, importFile)
//		f.mu.Unlock()
//		fs.mu.Unlock()
//		//		fs.mu.Unlock()
//	}
// }
