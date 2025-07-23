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

// Types for Symbols
type SymbolKind int

const (
	Definition SymbolKind = iota
	WithEnvironment
	LetRecEnvironment
	Case
	Iteration
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

func NewScope(parent *Scope) *Scope {
	return &Scope{
		Parent:  parent,
		Symbols: make(map[string]Symbol),
	}
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

type LibrarySymbol struct {
	BaseSymbol
	File util.Path
}

type ImportSymbol struct {
	BaseSymbol
	File util.Path
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
func (dg *DependencyGraph) GetImporters(path string) []string {
	dg.mu.RLock()
	defer dg.mu.RUnlock()

	importers := []string{}
	if s, ok := dg.importedBy[path]; ok {
		for importerPath := range s {
			importers = append(importers, importerPath)
		}
	}
	return importers
}

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
