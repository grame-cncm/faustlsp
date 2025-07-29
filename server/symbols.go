package server

import (
	"fmt"
	"log/slog"
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

type SymbolKind int

const (
	// Identifier is a simple identifier with no scope or expression
	Identifier SymbolKind = iota
	// Definition is a simple variable or function along with expression
	Definition

	// Function has single scope for arguments along with expression
	Function

	// Pattern scope has multiple scopes and expressions for each rule.
	Case

	// Just like Function, but without identifier
	Rule

	// with and letrec environments have scope as well as expression
	WithEnvironment
	LetRecEnvironment

	// Environment has a scope along with identifier it is assigned to
	Environment

	// Library has a file path along with identifier it is assigned to
	Library

	// Import simply has a file path
	Import
)

var symbolKindStrings = map[SymbolKind]string{
	Identifier:        "Identifier",
	Definition:        "Definition",
	Function:          "Function",
	Case:              "Case",
	Rule:              "Rule",
	WithEnvironment:   "WithEnvironment",
	LetRecEnvironment: "LetRecEnvironment",
	Environment:       "Environment",
	Library:           "Library",
	Import:            "Import",
}

func (k SymbolKind) String() string {
	s, ok := symbolKindStrings[k]
	if ok {
		return s
	}
	return "UnknownSymbolKind"
}

type Symbol struct {
	Kind  SymbolKind
	Loc   Location
	Ident string
	Scope *Scope

	// For Case's Rules
	Children []Symbol

	// Useful for populating reference map after parsing AST
	Expr *tree_sitter.Node

	// File path to import scope from
	File util.Path
}

func NewIdentifier(Loc Location, Ident string) Symbol {
	return Symbol{
		Kind:  Identifier,
		Loc:   Loc,
		Ident: Ident,
	}
}

func NewDefinition(Loc Location, Ident string, Expr *tree_sitter.Node) Symbol {
	return Symbol{
		Kind:  Definition,
		Loc:   Loc,
		Ident: Ident,
		Expr:  Expr,
	}
}

func NewFunction(Loc Location, Ident string, Scope *Scope, Expr *tree_sitter.Node) Symbol {
	return Symbol{
		Kind:  Function,
		Loc:   Loc,
		Ident: Ident,
		Scope: Scope,
		Expr:  Expr,
	}
}

func NewCase(Loc Location, Children []Symbol) Symbol {
	return Symbol{
		Kind: Case,
		Loc:  Loc,
		// For Case Rules
		Children: Children,
	}
}

func NewRule(Loc Location, Scope *Scope, Expr *tree_sitter.Node) Symbol {
	return Symbol{
		Kind:  Rule,
		Loc:   Loc,
		Scope: Scope,
		Expr:  Expr,
	}
}

func NewWithEnvironment(Loc Location, Scope *Scope, Expr *tree_sitter.Node) Symbol {
	return Symbol{
		Kind:  WithEnvironment,
		Loc:   Loc,
		Scope: Scope,
		Expr:  Expr,
	}
}

func NewLetRecEnvironment(Loc Location, Scope *Scope, Expr *tree_sitter.Node) Symbol {
	return Symbol{
		Kind:  LetRecEnvironment,
		Loc:   Loc,
		Scope: Scope,
		Expr:  Expr,
	}
}

func NewEnvironment(Loc Location, Ident string, Scope *Scope) Symbol {
	return Symbol{
		Kind:  Environment,
		Ident: Ident,
		Loc:   Loc,
		Scope: Scope,
	}
}

func NewLibrary(Loc Location, importedFile util.Path, Ident string) Symbol {
	return Symbol{
		Kind:  Library,
		Ident: Ident,
		Loc:   Loc,
		File:  importedFile,
	}
}

func NewImport(Loc Location, importedFile util.Path) Symbol {
	return Symbol{
		Kind: Import,
		Loc:  Loc,
		File: importedFile,
	}
}

type Location struct {
	File  util.Path
	Range transport.Range
}

type Scope struct {
	Parent   *Scope
	Symbols  []*Symbol
	Children []*Scope
	Range    transport.Range
}

func (s *Scope) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Any("symbols", s.Symbols),
		slog.Any("range", s.Range),
	)
}

func NewScope(parent *Scope, scopeRange transport.Range) *Scope {
	scope := Scope{
		Parent:   parent,
		Symbols:  []*Symbol{},
		Children: []*Scope{},
		Range:    scopeRange,
	}
	if parent != nil {
		parent.Children = append(parent.Children, &scope)
	}

	return &scope
}

func (scope *Scope) addSymbol(sym *Symbol) {
	scope.Symbols = append(scope.Symbols, sym)
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
	Files        *Files
	References   ReferenceMap
	Dependencies DependencyGraph
}

// This needs workspace to be able to resolve the file path
// Analyzes AST of a File and updates the store
func (workspace *Workspace) AnalyzeFile(f *File, store *Store) {
	// 1) First parse AST to our Symbols + descend into imports and analyzefiles that it imports
	// 2) Update Dependency Graph as we traverse
	// 3) After 1) and 2) are done, resolve all symbols as references

	var visited = make(map[util.Path]struct{})

	workspace.ParseFile(f, store, visited)
}

func (workspace *Workspace) ParseFile(f *File, store *Store, visited map[util.Path]struct{}) {
	f.mu.RLock()
	tree := parser.ParseTree(f.Content)
	root := tree.RootNode()
	scope := NewScope(nil, ToRange(root))
	visited[f.Handle.Path] = struct{}{}
	workspace.ParseASTNode(root, f, scope, store, visited)
	f.mu.RUnlock()
	tree.Close()
}

func (workspace *Workspace) ParseASTNode(node *tree_sitter.Node, currentFile *File, scope *Scope, store *Store, visited map[util.Path]struct{}) {
	// Parse Symbols recursively. Map from tree_sitter.Node -> a Symbol type
	if node == nil {
		logging.Logger.Error("AST Parsing Traversal Error: Node is nil", "node", node)
		return
	}

	name := node.GrammarName()

	switch name {
	case "definition":
		logging.Logger.Info("AST Traversal: Got definition")

		value := node.ChildByFieldName("value")
		ident := node.ChildByFieldName("variable")
		if value == nil {
			logging.Logger.Info("AST Traversal: Got definition without value. Ignoring.")
			return
		}

		valueGrammarName := value.GrammarName()
		identName := ident.Utf8Text(currentFile.Content)

		if valueGrammarName == "library" {
			logging.Logger.Info("AST Traversal: Got library")

			fileName := value.ChildByFieldName("filename")
			if fileName == nil {
				logging.Logger.Error("AST Traversal: Library definition without filename", "node", node)
				return
			}

			libraryFilePath := stripQuotes(fileName.Utf8Text(currentFile.Content))
			sym := NewLibrary(Location{
				File:  currentFile.Handle.Path,
				Range: ToRange(ident),
			}, libraryFilePath, identName)
			scope.addSymbol(&sym)
			logging.Logger.Info("Current scope values", "scope", scope)

			// TODO: Recursively parse the library file if it exists

		} else if valueGrammarName == "environment" {
			logging.Logger.Info("AST Traversal: Got environment")
			// Move to the environment node. For some reason, the environment node is the next sibling of the value node, which is just the "environment" keyword
			value = value.NextSibling()
			envScope := NewScope(scope, ToRange(value))

			// Value = (environment) node
			for i := uint(0); i < value.ChildCount(); i++ {
				// Parse each child of environment node
				logging.Logger.Info("AST Traversal: Parsing environment child", "child", value.Child(i).GrammarName())
				workspace.ParseASTNode(value.Child(i), currentFile, envScope, store, visited)
			}
			sym := NewEnvironment(
				Location{
					File:  currentFile.Handle.Path,
					Range: ToRange(ident),
				},
				identName,
				envScope,
			)
			scope.addSymbol(&sym)
		} else {
			if ident == nil {
				logging.Logger.Info("AST Traversal: Got definition without identifier. Ignoring.")
				return
			}

			sym := NewDefinition(
				Location{
					File:  currentFile.Handle.Path,
					Range: ToRange(ident),
				},
				identName,
				value)
			scope.addSymbol(&sym)
			logging.Logger.Info("Current symbol's expression", "expr", sym.Expr.GrammarName())
			logging.Logger.Info("Current scope values", "scope", scope)
			for i := uint(0); i < node.ChildCount(); i++ {
				workspace.ParseASTNode(node.Child(i), currentFile, scope, store, visited)
			}
		}
	case "function_definition":

		functionName := node.ChildByFieldName("name")
		if functionName == nil {
			logging.Logger.Error("AST Traversal: Function definition without name. Skipping")
			return
		}

		arguments := functionName.NextNamedSibling()
		if arguments == nil {
			logging.Logger.Error("AST Traversal: Function definition without arguments. Skipping")
			return
		}

		argumentsScope := NewScope(scope, ToRange(node))
		logging.Logger.Info("AST Traversal: Got function_definition", "arguments", arguments.GrammarName(), "functionName", functionName.Utf8Text(currentFile.Content))
		for i := uint(0); i < arguments.ChildCount(); i++ {

			argumentNode := arguments.Child(i)
			if !argumentNode.IsNamed() {
				continue
			}

			logging.Logger.Info("AST Traversal: Parsing function argument", "arg", arguments.Child(i).GrammarName())

			arg := NewIdentifier(
				Location{
					File:  currentFile.Handle.Path,
					Range: ToRange(argumentNode),
				},
				argumentNode.Utf8Text(currentFile.Content),
			)
			argumentsScope.addSymbol(&arg)
		}

		expression := node.ChildByFieldName("value")
		if expression == nil {
			logging.Logger.Error("AST Traversal: Function definition without expression. Skipping")
			return
		}

		functionNode := NewFunction(
			Location{
				File:  currentFile.Handle.Path,
				Range: ToRange(functionName),
			},
			functionName.Utf8Text(currentFile.Content),
			argumentsScope,
			expression,
		)

		scope.addSymbol(&functionNode)
		logging.Logger.Info("Current scope values", "scope", scope)

		// Treat it as a part of a pattern scope because arguments defined are only in function scope

		for i := uint(0); i < node.ChildCount(); i++ {
			workspace.ParseASTNode(node.Child(i), currentFile, scope, store, visited)
		}
	case "recinition":
		logging.Logger.Info("AST Traversal: Got recinition")
		ident := node.ChildByFieldName("name")
		expr := node.ChildByFieldName("expression")

		if ident == nil || expr == nil {
			logging.Logger.Error("AST Traversal: Recinition without ident or expr", "node is nil", ident == nil, "expr is nil", expr == nil)
			return
		}
		sym := NewDefinition(
			Location{
				File:  currentFile.Handle.Path,
				Range: ToRange(ident),
			},
			ident.Utf8Text(currentFile.Content),
			expr)
		scope.addSymbol(&sym)
		logging.Logger.Info("Current scope values", "scope", scope)

	case "with_environment":
		logging.Logger.Info("AST Traversal: Got with environment", "text", node.Utf8Text(currentFile.Content))

		expr := node.ChildByFieldName("expression")

		if expr == nil {
			logging.Logger.Error("AST Traversal: Environment without expression. Skipping")
			return
		}
		environment := node.ChildByFieldName("local_environment")
		if environment == nil {
			logging.Logger.Error("AST Traversal: Environment without local_environment. Skipping")
			return
		}

		withScope := NewScope(scope, ToRange(node))
		for i := uint(0); i < environment.ChildCount(); i++ {
			logging.Logger.Info("AST Traversal: Parsing child", "child", environment.Child(i).GrammarName())
			workspace.ParseASTNode(environment.Child(i), currentFile, withScope, store, visited)
		}

		sym := NewWithEnvironment(Location{
			File:  currentFile.Handle.Path,
			Range: ToRange(environment),
		}, withScope, expr)
		scope.addSymbol(&sym)
		logging.Logger.Info("Current scope values", "scope", scope)

	case "letrec_environment":
		logging.Logger.Info("AST Traversal: Got letrec environment", "text", node.Utf8Text(currentFile.Content))
		expr := node.ChildByFieldName("expression")
		if expr == nil {
			logging.Logger.Error("AST Traversal: LetRec environment without expression. Skipping")
			return
		}
		environment := node.ChildByFieldName("local_environment")
		if environment == nil {
			logging.Logger.Error("AST Traversal: LetRec environment without local_environment. Skipping")
			return
		}

		letRecScope := NewScope(scope, ToRange(node))
		for i := uint(0); i < environment.ChildCount(); i++ {
			logging.Logger.Info("AST Traversal: Parsing child", "child", environment.Child(i).GrammarName())
			workspace.ParseASTNode(environment.Child(i), currentFile, letRecScope, store, visited)
		}

		sym := NewLetRecEnvironment(Location{
			File:  currentFile.Handle.Path,
			Range: ToRange(environment),
		}, letRecScope, expr)
		scope.addSymbol(&sym)
		logging.Logger.Info("Current scope values", "scope", scope)

	// Import statement
	case "file_import":
		fileNode := node.ChildByFieldName("filename")
		if fileNode == nil {
			logging.Logger.Info("AST Traversal: Got import statement without importing file. Ignoring.")
			return
		}

		// Strip quotes as file name comes as "file_name" not just file_name in tree_sitter grammar
		file := stripQuotes(fileNode.Utf8Text(currentFile.Content))
		resolvedPath, _ := workspace.ResolveFilePath(file, workspace.Root)
		logging.Logger.Info("AST Traversal: Got import statement", "file", resolvedPath)

		sym := NewImport(
			Location{
				File:  currentFile.Handle.Path,
				Range: ToRange(node),
			},
			resolvedPath)
		scope.addSymbol(&sym)
		logging.Logger.Info("Current scope values", "scope", scope)
		// TODO: Recursively parse the imported file if it exists

	case "iteration":
		logging.Logger.Info("AST Traversal: Got iteration node")

		for i := uint(0); i < node.ChildCount(); i++ {
			workspace.ParseASTNode(node.Child(i), currentFile, scope, store, visited)
		}
	case "pattern":
		logging.Logger.Info("AST Traversal: Got pattern node")

		caseRules := []Symbol{}

		rules := node.NamedChild(0)

		if rules == nil {
			logging.Logger.Error("AST Traversal: Pattern node without rules. Skipping")
			return
		}

		for i := uint(0); i < rules.NamedChildCount(); i++ {
			ruleNode := rules.NamedChild(i)

			if ruleNode == nil {
				logging.Logger.Error("AST Traversal: Pattern node with nil child. Skipping")
				continue
			}
			logging.Logger.Info("AST Traversal: Parsing rule", "rule", ruleNode.NamedChild(0).ToSexp())
			if ruleNode.GrammarName() != "rule" {
				logging.Logger.Error("AST Traversal: Pattern node with non-rule child. Skipping", "child", ruleNode.GrammarName())
				continue
			}

			arguments := ruleNode.NamedChild(0) // arguments are the first child of a rule node
			if arguments == nil {
				logging.Logger.Error("AST Traversal: Rule without arguments. Skipping")
				continue
			}

			expression := ruleNode.ChildByFieldName("expression")
			if expression == nil {
				logging.Logger.Error("AST Traversal: Rule without expression. Skipping")
				continue
			}

			ruleScope := NewScope(scope, ToRange(ruleNode))
			for j := uint(0); j < arguments.ChildCount(); j++ {
				argument := arguments.Child(j)
				argumentSym := NewIdentifier(
					Location{
						File:  currentFile.Handle.Path,
						Range: ToRange(argument),
					},
					argument.Utf8Text(currentFile.Content))
				ruleScope.addSymbol(&argumentSym)
			}

			ruleSym := NewRule(Location{
				File:  currentFile.Handle.Path,
				Range: ToRange(ruleNode),
			}, ruleScope, expression)

			caseRules = append(caseRules, ruleSym)
			logging.Logger.Info("AST Traversal: Parsed rule", "rule", ruleSym.Ident, "scope", ruleSym.Scope)
		}

		caseSymbol := NewCase(
			Location{
				File:  currentFile.Handle.Path,
				Range: ToRange(node),
			},
			caseRules)
		scope.addSymbol(&caseSymbol)

		logging.Logger.Info("AST Traversal: Parsed pattern", "case_rules", len(caseSymbol.Children))
		logging.Logger.Info("Current scope values", "scope", scope)
	default:
		for i := uint(0); i < node.ChildCount(); i++ {
			workspace.ParseASTNode(node.Child(i), currentFile, scope, store, visited)
		}
	}
}

func ToRange(node *tree_sitter.Node) transport.Range {
	start := node.StartPosition()
	end := node.EndPosition()

	return transport.Range{
		Start: transport.Position{Line: uint32(start.Row), Character: uint32(start.Column)},
		End:   transport.Position{Line: uint32(end.Row), Character: uint32(end.Column)},
	}
}

func stripQuotes(s string) string {
	stripped := s[1 : len(s)-1]
	return stripped
}

func (workspace *Workspace) ParseFileAndAddToStore(f *File, s *Server) {
	f.mu.RLock()
	f.Scope = NewScope(nil, transport.Range{})
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
					traverseAST(expression, NewScope(scope, transport.Range{}))
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
	faustCommand := w.Config.Command
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
// Returns the path along with the directory/workspace path the file was found in
func (w *Workspace) ResolveFilePath(relPath util.Path, rootDir util.Path) (path util.Path, dir util.Path) {
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

func FindDefinition(ident string, scope *Scope, store *Store) (Location, error) {
	// Keep looking up in scope to find symbol
	// Question: How do we handle library symbols ?
	// Solution: os.osc. Split at first . and find symbol at left. Then recursively find symbol at right in the library definition's file
	// Normal import statements is just looking in upper scope.
	// Note: To avoid cycles, keep track of traversed files
	if scope == nil {
		return Location{}, fmt.Errorf("Invalid scope")
	}

	for _, symbol := range scope.Symbols {
		if symbol.Kind == Import {
			f, ok := store.Files.GetFromPath(symbol.File)
			if ok {
				found, err := FindDefinition(ident, f.Scope, store)
				if err != nil {
					return found, nil
				}
			}
		}
		if symbol.Ident == ident {
			return symbol.Loc, nil
		}
	}

	if scope.Parent != nil {
		return FindDefinition(ident, scope.Parent, store)
	} else {
		return Location{}, fmt.Errorf("Couldn't find symbol")
	}

}
