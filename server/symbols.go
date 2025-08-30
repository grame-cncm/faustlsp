package server

import (
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

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

	// Iteration (par, seq, sum, prod)
	Iteration

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
	Iteration:         "Iteration",
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
	// For with_environments and letrec_environments, useful for references and environment symbols
	Expression *Scope

	// File path to import scope from
	File util.Path

	// Documentation
	Docs Documentation
}

type Documentation struct {
	Full  string
	Usage string
}

func ParseDocumentation(node *tree_sitter.Node, content []byte) Documentation {
	if node == nil {
		return Documentation{Full: "", Usage: ""}
	}

	docContent := []string{}
	curr := node

	// Traverse previous siblings until we find a non-comment node
	for {
		curr = curr.PrevSibling()
		if curr == nil {
			break
		}
		if curr.GrammarName() != "comment" {
			break
		}

		lineContent := curr.Utf8Text(content)
		lineContent = lineContent[len("//"):]
		// Double spaces for markdown
		docContent = slices.Insert(docContent, 0, lineContent)
	}

	usage := ""
	if len(docContent) > 1 {
		usage = docContent[1]
	} else if len(docContent) == 1 {
		usage = docContent[0]
	}

	doc := Documentation{
		Full:  strings.Join(docContent, "  \n"),
		Usage: usage,
	}
	logging.Logger.Info("Parsed docs", "documentation", doc)
	return doc
}

func containsLetters(str string) bool {
	for _, c := range str {
		if !unicode.IsLetter(c) {
			return false
		}
	}
	return true
}

func NewIdentifier(Loc Location, Ident string) Symbol {
	return Symbol{
		Kind:  Identifier,
		Loc:   Loc,
		Ident: Ident,
	}
}

func NewDefinition(Loc Location, Ident string, Expr *tree_sitter.Node, Expression *Scope, Docs Documentation) Symbol {
	return Symbol{
		Kind:       Definition,
		Loc:        Loc,
		Ident:      Ident,
		Expr:       Expr,
		Expression: Expression,
		Docs:       Docs,
	}
}

func NewFunction(Loc Location, Ident string, Scope *Scope, Expr *tree_sitter.Node, Expression *Scope, Docs Documentation) Symbol {
	return Symbol{
		Kind:       Function,
		Loc:        Loc,
		Ident:      Ident,
		Scope:      Scope,
		Expression: Expression,
		Docs:       Docs,
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

func NewIteration(Loc Location, Scope *Scope, Expr *tree_sitter.Node) Symbol {
	return Symbol{
		Kind:  Iteration,
		Loc:   Loc,
		Scope: Scope,
		Expr:  Expr,
	}
}

func NewWithEnvironment(Loc Location, Scope *Scope, Expr *tree_sitter.Node, Expression *Scope) Symbol {
	return Symbol{
		Kind:       WithEnvironment,
		Loc:        Loc,
		Scope:      Scope,
		Expr:       Expr,
		Expression: Expression,
	}
}

func NewLetRecEnvironment(Loc Location, Scope *Scope, Expr *tree_sitter.Node, Expression *Scope) Symbol {
	return Symbol{
		Kind:       LetRecEnvironment,
		Loc:        Loc,
		Scope:      Scope,
		Expr:       Expr,
		Expression: Expression,
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
	// If found and string != "", it is a library import (used for reference finding)
	importedBy map[string]map[string]string

	// Tracks files currently being analyzed/processed to detect cycles.
	// Maps file Path to true if it's currently in the analysis stack.
	processing map[string]bool
}

func NewDependencyGraph() DependencyGraph {
	return DependencyGraph{
		imports:    make(map[string]map[string]struct{}),
		importedBy: make(map[string]map[string]string),
		processing: make(map[string]bool),
	}
}

// AddDependency records that 'importerPath' imports 'importedPath'.
func (dg *DependencyGraph) AddDependency(importerPath, importedPath util.Path) {
	dg.mu.Lock()
	defer dg.mu.Unlock()

	if _, ok := dg.imports[importerPath]; !ok {
		dg.imports[importerPath] = make(map[string]struct{})
	}
	dg.imports[importerPath][importedPath] = struct{}{}

	if _, ok := dg.importedBy[importedPath]; !ok {
		dg.importedBy[importedPath] = make(map[string]string)
	}
	dg.importedBy[importedPath][importerPath] = ""
}

func (dg *DependencyGraph) AddLibraryDependency(importerPath, importedPath util.Path, library string) {
	dg.mu.Lock()
	defer dg.mu.Unlock()

	if _, ok := dg.imports[importerPath]; !ok {
		dg.imports[importerPath] = make(map[string]struct{})
	}
	dg.imports[importerPath][importedPath] = struct{}{}

	if _, ok := dg.importedBy[importedPath]; !ok {
		dg.importedBy[importedPath] = make(map[string]string)
	}
	dg.importedBy[importedPath][importerPath] = library
}

// Call this before re-analyzing a file, as its imports might have changed.
func (dg *DependencyGraph) RemoveDependenciesForFile(path util.Path) {
	dg.mu.Lock()
	defer dg.mu.Unlock()

	// Remove its outgoing dependencies
	if importedPaths, ok := dg.imports[path]; ok {
		for importedPath := range importedPaths {
			delete(dg.importedBy[importedPath], path) // Remove self from imported's list
			if len(dg.importedBy[importedPath]) == 0 {
				delete(dg.importedBy, importedPath) // Clean up empty sets
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
	Cache        map[[sha256.Size]byte]*Scope
}

// This needs workspace to be able to resolve the file path
// Analyzes AST of a File and updates the store
func (workspace *Workspace) AnalyzeFile(f *File, store *Store) {
	// 3) After 1) and 2) are done, resolve all symbols as references

	var visited = make(map[util.Path]struct{})

	// Stack for files to parse after current file
	var fileChan = make(chan string)

	// Parse through file import tree asynchronously to speed up parsing times using a pipeline
	go func() {
		for {
			select {
			case currentFile := <-fileChan:
				logging.Logger.Info("Parsing file", "file", currentFile)
				f, ok := store.Files.GetFromPath(currentFile)
				//logging.Logger.Info("AST Traversal: Got library definition", "file", current, "ident", identName)
				if ok {
					go workspace.ParseFile(f, store, visited, fileChan)

				} else {
					store.Files.OpenFromPath(currentFile)
					f, ok := store.Files.GetFromPath(currentFile)
					if ok {
						go workspace.ParseFile(f, store, visited, fileChan)
					}

				}
			// Close file channel after 30 seconds
			// TODO: Find way to close channel when all files are done parsing
			case <-time.After(5 * time.Second):
				logging.Logger.Info("Closing file channel as nothing received for 5 seconds")
				close(fileChan)
				return
			}
		}
	}()

	logging.Logger.Info("Starting to analyze file", "path", f.Handle.Path)
	workspace.ParseFile(f, store, visited, fileChan)

	logging.Logger.Info("AST Parsing completed for file", "file", f.Handle.Path)
	//	logging.Logger.Info("Dependency Graph", "graph", store.Dependencies.imports)
}

func (workspace *Workspace) ParseFile(f *File, store *Store, visited map[util.Path]struct{}, fileChan chan string) {
	// If file is already visited, skip it
	if _, ok := visited[f.Handle.Path]; !ok {
		f.mu.Lock()
		// Check if file content of this type is already parsed
		scope, ok := store.Cache[f.Hash]
		if ok {
			logging.Logger.Info("File already parsed, using cached scope", "file", f.Handle.Path)
			f.Scope = scope
			f.mu.Unlock()
		} else {

			tree := parser.ParseTree(f.Content)
			root := tree.RootNode()
			scope := NewScope(nil, ToRange(root))
			visited[f.Handle.Path] = struct{}{}
			workspace.ParseASTNode(root, f, scope, store, visited, fileChan)
			f.Scope = scope
			store.Cache[f.Hash] = scope
			f.mu.Unlock()

			//			tree.Close()
			logging.Logger.Info("Parsed file", "path", f.Handle.Path)
		}
	} else {
		logging.Logger.Info("Skipping file as it is already visited", "file", f.Handle.Path)
	}

}

func (workspace *Workspace) ParseASTNode(node *tree_sitter.Node, currentFile *File, scope *Scope, store *Store, visited map[util.Path]struct{}, fileChan chan string) {
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
			resolvedPath, _ := workspace.ResolveFilePath(libraryFilePath, workspace.Root)

			logging.Logger.Info("AST Traversal: Got library definition", "file", resolvedPath, "ident", identName)
			fileChan <- resolvedPath

			logging.Logger.Info("AST Traversal: Got library definition", "file", resolvedPath, "ident", identName)
			store.Dependencies.RemoveDependenciesForFile(currentFile.Handle.Path)
			store.Dependencies.AddLibraryDependency(currentFile.Handle.Path, resolvedPath, identName)

			sym := NewLibrary(Location{
				File:  currentFile.Handle.Path,
				Range: ToRange(ident),
			}, resolvedPath, identName)
			scope.addSymbol(&sym)
			logging.Logger.Info("Current scope values", "scope", scope)

		} else if valueGrammarName == "environment" {
			logging.Logger.Info("AST Traversal: Got environment")
			// Move to the environment node. For some reason, the environment node is the next sibling of the value node, which is just the "environment" keyword
			value = value.NextSibling()
			envScope := NewScope(scope, ToRange(value))

			// Value = (environment) node
			for i := uint(0); i < value.ChildCount(); i++ {
				// Parse each child of environment node
				logging.Logger.Info("AST Traversal: Parsing environment child", "child", value.Child(i).GrammarName())
				workspace.ParseASTNode(value.Child(i), currentFile, envScope, store, visited, fileChan)
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

			logging.Logger.Info("Current scope values", "scope", scope)
			expr := NewScope(scope, ToRange(value))
			for i := uint(0); i < node.ChildCount(); i++ {
				workspace.ParseASTNode(node.Child(i), currentFile, expr, store, visited, fileChan)
			}
			sym := NewDefinition(
				Location{
					File:  currentFile.Handle.Path,
					Range: ToRange(node),
				},
				identName,
				value, expr, ParseDocumentation(node, currentFile.Content))
			scope.addSymbol(&sym)
		}
	case "environment":
		logging.Logger.Info("AST Traversal: Parsing Environment without identifier", "environment", node.Utf8Text(currentFile.Content))
		node = node.NextSibling()
		if node == nil {
			logging.Logger.Info("AST Traversal: Got environment without definitions. Ignoring.")
			return
		}
		envScope := NewScope(scope, ToRange(node))

		for i := uint(0); i < node.ChildCount(); i++ {
			workspace.ParseASTNode(node.Child(i), currentFile, envScope, store, visited, fileChan)
		}
		sym := NewEnvironment(
			Location{
				File:  currentFile.Handle.Path,
				Range: ToRange(node),
			},
			"",
			envScope,
		)
		scope.addSymbol(&sym)
		logging.Logger.Info("AST Traversal: Parsed environment", "locatio", sym.Loc)

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

			logging.Logger.Info("AST Traversal: Parsing function argument", "arg", argumentNode.GrammarName(), "content", argumentNode.Utf8Text(currentFile.Content))

			arg := NewIdentifier(
				Location{
					File:  currentFile.Handle.Path,
					Range: ToRange(argumentNode),
				},
				argumentNode.Utf8Text(currentFile.Content),
			)
			argumentsScope.addSymbol(&arg)
		}
		if len(argumentsScope.Symbols) > 0 {
			logging.Logger.Info("Arguments Scope", "scope", argumentsScope.Symbols[0].Ident)
		}

		expression := node.ChildByFieldName("value")
		if expression == nil {
			logging.Logger.Error("AST Traversal: Function definition without expression. Skipping")
			return
		}

		// Treat it as a part of a pattern scope because arguments defined are only in function scope
		exprScope := NewScope(scope, ToRange(node))
		logging.Logger.Info("Parsing function value using separate scope")
		for i := uint(0); i < node.ChildCount(); i++ {
			workspace.ParseASTNode(node.Child(i), currentFile, exprScope, store, visited, fileChan)
		}

		functionNode := NewFunction(
			Location{
				File:  currentFile.Handle.Path,
				Range: ToRange(node),
			},
			functionName.Utf8Text(currentFile.Content),
			argumentsScope,
			expression,
			exprScope,
			ParseDocumentation(node, currentFile.Content),
		)

		scope.addSymbol(&functionNode)
		logging.Logger.Info("Current scope values", "scope_children", len(scope.Children), "scope_symbols", len(scope.Symbols))
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
			expr, nil, ParseDocumentation(ident, currentFile.Content))
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
		for i := uint(0); i < environment.NamedChildCount(); i++ {
			logging.Logger.Info("AST Traversal: Parsing environment definition", "child", environment.NamedChild(i).GrammarName())
			workspace.ParseASTNode(environment.NamedChild(i), currentFile, withScope, store, visited, fileChan)
		}

		exprScope := NewScope(scope, ToRange(node))
		logging.Logger.Info("AST Traversal: Parsing expr definition", "child", expr.GrammarName())
		workspace.ParseASTNode(expr, currentFile, exprScope, store, visited, fileChan)

		sym := NewWithEnvironment(Location{
			File:  currentFile.Handle.Path,
			Range: ToRange(node),
		}, withScope, expr, exprScope)
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
			workspace.ParseASTNode(environment.Child(i), currentFile, letRecScope, store, visited, fileChan)
		}

		exprScope := NewScope(scope, ToRange(node))
		workspace.ParseASTNode(expr, currentFile, exprScope, store, visited, fileChan)

		sym := NewLetRecEnvironment(Location{
			File:  currentFile.Handle.Path,
			Range: ToRange(node),
		}, letRecScope, expr, exprScope)
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
		logging.Logger.Info("AST Traversal: Got import statement. Going through tree", "file", resolvedPath)

		fileChan <- resolvedPath

		store.Dependencies.RemoveDependenciesForFile(currentFile.Handle.Path)
		store.Dependencies.AddDependency(currentFile.Handle.Path, resolvedPath)

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

		currentIter := node.ChildByFieldName("current_iter")
		if currentIter == nil {
			logging.Logger.Error("AST Traversal: Iteration node without current_iter. Skipping")
			return
		}

		expr := node.ChildByFieldName("expression")
		if expr == nil {
			logging.Logger.Error("AST Traversal: Iteration node without expression. Skipping")
			return
		}

		// Create a new scope for the iteration
		iterScope := NewScope(scope, ToRange(node))

		currentIterIdent := NewIdentifier(
			Location{
				File:  currentFile.Handle.Path,
				Range: ToRange(currentIter),
			},
			currentIter.Utf8Text(currentFile.Content))
		iterScope.addSymbol(&currentIterIdent)

		iterSym := NewIteration(
			Location{
				File:  currentFile.Handle.Path,
				Range: ToRange(node),
			},
			iterScope,
			expr)

		scope.addSymbol(&iterSym)
		logging.Logger.Info("Parsed iteration", "current_iter", currentIterIdent.Ident, "scope", iterScope)
		logging.Logger.Info("Current scope values", "scope", scope)
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

			if ruleNode.GrammarName() != "rule" {
				logging.Logger.Error("AST Traversal: Pattern node with non-rule child. Skipping", "child", ruleNode.GrammarName())
				continue
			}

			arguments := ruleNode.NamedChild(0) // arguments are the first child of a rule node
			if arguments == nil {
				logging.Logger.Error("AST Traversal: Rule without arguments. Skipping")
				continue
			}
			logging.Logger.Info("AST Traversal: Parsing rule", "rule", arguments.ToSexp())

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
			workspace.ParseASTNode(node.Child(i), currentFile, scope, store, visited, fileChan)
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
	//	logging.Logger.Info("Trying path", "path", path1)
	if util.IsValidPath(path1) {
		return path1, rootDir
	}

	// File in Faust System Library DSP directory
	faustDSPDir := w.GetFaustDSPDir()
	path2 := filepath.Join(faustDSPDir, relPath)
	//	logging.Logger.Info("Trying path", "path", path2)
	if util.IsValidPath(path2) {
		return path2, faustDSPDir
	}

	logging.Logger.Info("Couldn't resolve file path")
	return "", ""
}

func FindSymbol(ident string, scope *Scope, store *Store) (Symbol, error) {
	var visited = make(map[util.Path]struct{})

	return FindSymbolHelper(ident, scope, store, &visited)
}

func FindSymbolHelper(ident string, scope *Scope, store *Store, visited *map[util.Path]struct{}) (Symbol, error) {
	if scope == nil {
		return Symbol{}, fmt.Errorf("Invalid scope")
	}

	// 1) Check current scope's definitions for this symbol
	for _, symbol := range scope.Symbols {

		if symbol.Ident == ident {
			return *symbol, nil
		}
	}

	// 2) Check imported files for this symbol
	// TODO: Instead of 2 loops, get import symbols in the first loop itself and iterate through that
	logging.Logger.Info("Symbol not in scope, checking import statements")
	for i, symbol := range scope.Symbols {

		if symbol.Kind == Import {
			logging.Logger.Info("Symbol type", "type", symbol.Kind.String(), "index", i)
			f, ok := store.Files.GetFromPath(symbol.File)
			if ok {
				logging.Logger.Info("Found import statement, checking in file", "path", f.Handle.Path)
				found, err := FindSymbolHelper(ident, f.Scope, store, visited)
				if err == nil {
					return found, nil
				}
			}
		}
	}

	if scope.Parent != nil {
		logging.Logger.Info("Going to parent to find", "ident", ident)
		return FindSymbolHelper(ident, scope.Parent, store, visited)
	} else {
		return Symbol{}, fmt.Errorf("Couldn't find symbol")
	}

}

func FindSymbolDefinition(ident string, scope *Scope, store *Store) (Symbol, error) {
	identSplit := strings.Split(ident, ".")

	if len(identSplit) > 1 {
		logging.Logger.Info("Resolving library symbol", "symbol", identSplit)
		for i := range len(identSplit) - 1 {
			libIdent := identSplit[i]

			// Resolve as Environment
			sym, err := FindEnvironmentIdent(libIdent, scope, store)
			logging.Logger.Info("Resolved environment", "env", libIdent, "sym", sym.Ident, "loc", sym.Loc)
			if err == nil {
				scope = sym.Scope
				continue
			}

			// Resolve as Library if not resolved as environment
			file, err := FindLibraryIdent(libIdent, scope, store)
			if err != nil {
				break
			}
			logging.Logger.Info("Resolved library environment", "env", libIdent, "location", file)
			f, ok := store.Files.GetFromPath(file)
			if ok {
				f.mu.RLock()
				logging.Logger.Info("Setting New Scope to", "path", file)
				scope = f.Scope
				f.mu.RUnlock()
				if scope == nil {
					break
				}
			}
		}
	}
	ident = identSplit[len(identSplit)-1]

	return FindSymbol(ident, scope, store)
}

func FindDefinition(ident string, scope *Scope, store *Store) (Location, error) {
	sym, err := FindSymbol(ident, scope, store)
	return sym.Loc, err
}

func FindDocs(ident string, scope *Scope, store *Store) (string, error) {
	sym, err := FindSymbol(ident, scope, store)
	return sym.Docs.Full, err
}

func FindEnvironmentIdent(ident string, scope *Scope, store *Store) (Symbol, error) {
	var visited = make(map[util.Path]struct{})

	return FindEnvironmentHelper(ident, scope, store, &visited)
}

func FindEnvironmentHelper(ident string, scope *Scope, store *Store, visited *map[util.Path]struct{}) (Symbol, error) {
	if scope == nil {
		return Symbol{}, fmt.Errorf("Invalid scope")
	}

	// 1) Check current scope's definitions for this symbol
	for _, symbol := range scope.Symbols {
		logging.Logger.Info("Comparing with current symbol", "symbol", symbol.Ident, "expected", ident)
		if symbol.Ident == ident {
			logging.Logger.Info("Found symbol, now looking deeper to find environment", "sym", ident)
			return FindFirstEnvironment(symbol)
		}
	}

	// 2) Check imported files for this symbol
	// TODO: Instead of 2 loops, get import symbols in the first loop itself and iterate through that
	logging.Logger.Info("Symbol not in scope, checking import statements")
	for i, symbol := range scope.Symbols {

		if symbol.Kind == Import {
			logging.Logger.Info("Symbol type", "type", symbol.Kind.String(), "index", i)
			f, ok := store.Files.GetFromPath(symbol.File)
			if ok {
				logging.Logger.Info("Found import statement, checking in file", "path", f.Handle.Path)
				found, err := FindEnvironmentHelper(ident, f.Scope, store, visited)
				if err == nil {
					return found, nil
				}
			}
		}
	}

	if scope.Parent != nil {
		logging.Logger.Info("Going to parent to find", "ident", ident)
		return FindEnvironmentHelper(ident, scope.Parent, store, visited)
	} else {
		return Symbol{}, fmt.Errorf("Couldn't find symbol")
	}

}

func FindFirstEnvironment(sym *Symbol) (Symbol, error) {
	switch sym.Kind {
	case Environment:
		//		logging.Logger.Info("Already environment symbol, returning", "env", sym.Loc.Range)
		return *sym, nil
	case WithEnvironment, LetRecEnvironment:
		//		logging.Logger.Info("With Environment, looking in it's children")
		for _, sym := range sym.Expression.Symbols {
			return FindFirstEnvironment(sym)
		}
	case Function, Definition:
		//		logging.Logger.Info("Definition, looking in it's children")
		for _, sym := range sym.Expression.Symbols {
			return FindFirstEnvironment(sym)
		}
	default:
		//		logging.Logger.Info("Got unwanted symbol, ignoring", "kind", sym.Kind.String(), "loc", sym.Loc)
	}
	return Symbol{}, fmt.Errorf("Couldn't find environment in symbol")

}

func FindLibraryIdent(ident string, scope *Scope, store *Store) (util.Path, error) {
	var visited = make(map[util.Path]struct{})

	return FindLibraryHelper(ident, scope, store, &visited)
}

func FindLibraryHelper(ident string, scope *Scope, store *Store, visited *map[util.Path]struct{}) (util.Path, error) {
	if scope == nil {
		return "", fmt.Errorf("Invalid scope")
	}

	// 1) Check current scope's definitions for this symbol
	for _, symbol := range scope.Symbols {
		logging.Logger.Info("Comparing with current symbol", "symbol", symbol.Ident, "expected", ident)
		if symbol.Ident == ident {
			return symbol.File, nil
		}
	}

	// 2) Check imported files for this symbol
	// TODO: Instead of 2 loops, get import symbols in the first loop itself and iterate through that
	logging.Logger.Info("Symbol not in scope, checking import statements")
	for i, symbol := range scope.Symbols {
		if symbol.Kind == Import {
			logging.Logger.Info("Symbol type", "type", symbol.Kind.String(), "index", i)
			f, ok := store.Files.GetFromPath(symbol.File)
			if ok {
				logging.Logger.Info("Found import statement, checking in file", "path", f.Handle.Path)
				found, err := FindLibraryHelper(ident, f.Scope, store, visited)
				if err == nil {
					return found, nil
				}
			}
		}
	}

	if scope.Parent != nil {
		logging.Logger.Info("Going to parent to find", "ident", ident)
		return FindLibraryHelper(ident, scope.Parent, store, visited)
	} else {
		return "", fmt.Errorf("Couldn't find symbol")
	}

}

type CompletionSym struct {
	name string
	docs Documentation
}

func GetPossibleSymbols(pos transport.Position, filePath util.Path, store *Store, encoding string) []CompletionSym {
	f, ok := store.Files.GetFromPath(filePath)
	if !ok {
		logging.Logger.Info("Couldn't find file", "path", filePath)
		return []CompletionSym{}
	}

	// 1) Get scope at position
	offset, err := PositionToOffset(pos, string(f.Content), encoding)
	if err != nil {
		logging.Logger.Info("Couldn't convert position to offset", "pos", pos, "err", err)
		return []CompletionSym{}
	}

	identifier, scope := FindSymbolScopeAtOffset(f.Content, f.Scope, offset, string(store.Files.encoding))
	if scope == nil {
		logging.Logger.Info("Couldn't find scope at position", "pos", pos, "offset", offset)
		return []CompletionSym{}
	}
	logging.Logger.Info("Found identifier at position", "ident", identifier, "scope_range", scope.Range, "len", len(scope.Symbols))

	// 2) Split identifier by '.' to get symbol tree and find scope of last identifier
	if identifier == "" {
		logging.Logger.Info("No identifier found at position, returning all symbols possible in current scope", "pos", pos, "offset", offset)

		availableSymbols := []CompletionSym{}
		for {
			if scope == nil {
				break
			}
			availableSymbols = append(availableSymbols, FindSymbolsNew(scope, "", store, make(map[util.Path]struct{}))...)
			scope = scope.Parent
		}
		return availableSymbols
	}
	if identifier[len(identifier)-1] == '.' {
		// Remove trailing '.' if any
		// Example: a.f. -> a.f
		// This is because completion is requested after '.'
		logging.Logger.Info("Removing trailing '.' from identifier", "ident", identifier)
		identifier = identifier[:len(identifier)-1]
		sym, err := FindSymbolDefinition(identifier, scope, store)
		if err != nil {
			logging.Logger.Info("Couldn't find symbol definition for identifier, checking with previous identifier", "ident", identifier, "err", err)
			identifierSplit := strings.Split(identifier, ".")
			if len(identifierSplit) > 2 {
				identifier = strings.Join(identifierSplit[:len(identifierSplit)-1], ".")
				sym, err = FindSymbolDefinition(identifier, scope, store)
				if err != nil {
					logging.Logger.Info("Couldn't find symbol definition for identifier", "ident", identifier, "err", err)
					return []CompletionSym{}
				}
			} else {
				return []CompletionSym{}
			}
		}
		logging.Logger.Info("Found symbol definition for identifier", "ident", identifier, "loc", sym.Loc)

		if sym.Kind == Library {
			logging.Logger.Info("Identifier is a library, getting symbols from file", "file", sym.File)
			f, ok := store.Files.GetFromPath(sym.File)
			if ok {
				f.mu.RLock()
				syms := FindSymbolsNew(f.Scope, "", store, make(map[util.Path]struct{}))
				f.mu.RUnlock()
				return syms
			} else {
				logging.Logger.Info("Couldn't find file for library", "file", sym.File)
				return []CompletionSym{}
			}
		} else {
			env, err := FindEnvironmentIdent(identifier, scope, store)
			if err == nil {
				return FindSymbolsNew(env.Scope, "", store, make(map[util.Path]struct{}))
			}
			return []CompletionSym{}
		}
	} else {
		logging.Logger.Info("Identifier doesn't end with '.', returning all symbols in current scope", "ident", identifier)
		availableSymbols := []CompletionSym{}
		for {
			if scope == nil {
				break
			}
			availableSymbols = append(availableSymbols, FindSymbolsNew(scope, "", store, make(map[util.Path]struct{}))...)
			scope = scope.Parent
		}
		return availableSymbols
	}
}

func JoinEnvIdent(parentSymbol, childSymbol string) string {
	if parentSymbol == "" {
		return childSymbol
	}
	return parentSymbol + "." + childSymbol
}

func AddEnvIdents(symbols []CompletionSym, parentSymbol string) []CompletionSym {
	for i, symbol := range symbols {
		sym := symbols[i]
		sym.name = JoinEnvIdent(parentSymbol, symbol.name)
		symbols[i] = sym
	}

	return symbols
}

func NewCompletionSym(sym *Symbol) CompletionSym {
	return CompletionSym{name: sym.Ident, docs: sym.Docs}
}

func FindSymbolsNew(scope *Scope, parentSymbol string, store *Store, visited map[util.Path]struct{}) []CompletionSym {
	symbols := []CompletionSym{}

	for _, sym := range scope.Symbols {
		logging.Logger.Info("Found symbol in scope", "symbol", sym.Ident, "kind", sym.Kind.String(), "loc", sym.Loc)
		if sym.Ident != "" {
			symbols = append(symbols, NewCompletionSym(sym))
		}
		if sym.Kind == Definition || sym.Kind == Function {
			env, err := FindFirstEnvironment(sym)
			if err != nil {
				continue
			}
			childSyms := FindSymbolsNew(env.Scope, JoinEnvIdent(parentSymbol, sym.Ident), store, visited)
			childSyms = AddEnvIdents(childSyms, JoinEnvIdent(parentSymbol, sym.Ident))
			symbols = slices.Concat(symbols, childSyms)

		}
		if sym.Kind == WithEnvironment || sym.Kind == LetRecEnvironment {
			// Find lowest environment
			env, err := FindFirstEnvironment(sym)
			if err != nil {
				continue
			}

			childSyms := FindSymbolsNew(env.Scope, JoinEnvIdent(parentSymbol, sym.Ident), store, visited)
			childSyms = AddEnvIdents(childSyms, JoinEnvIdent(parentSymbol, sym.Ident))
			symbols = slices.Concat(symbols, childSyms)
		}
		if sym.Kind == Import {
			symbols = slices.Concat(symbols, FindSymbolsInFile(sym, parentSymbol, store, visited))
		}
	}

	return symbols
}

func FindSymbolsInFile(sym *Symbol, parentSymbol string, store *Store, visited map[util.Path]struct{}) []CompletionSym {
	// Used for adding symbols from other files when an import or library statement is encountered
	symbols := []CompletionSym{}

	libPath := sym.File
	_, ok := visited[libPath]
	if !ok {
		logging.Logger.Info("Visiting file for the first time", "lib", libPath, "parentSymbol", parentSymbol)
		visited[libPath] = struct{}{}

		f, ok := store.Files.GetFromPath(libPath)
		if ok {
			f.mu.RLock()
			symbols = FindSymbolsNew(f.Scope, parentSymbol, store, visited)
			f.mu.RUnlock()
		}

	} else {
		logging.Logger.Info("File already visited", "path", libPath)

	}

	return symbols
}

func FindSymbolScope(content []byte, scope *Scope, offset uint) (string, *Scope) {
	tree := parser.ParseTree(content)
	fileAST := tree.RootNode()
	defer tree.Close()
	node := fileAST.DescendantForByteRange(offset, offset)
	logging.Logger.Info("Got descendant node as", "type", node.GrammarName(), "content", node.Utf8Text(content), "location", ToRange(node))
	switch node.GrammarName() {
	case "identifier":
		// If parent is access, keep finding scopes for each environment monoidically (e.g. lib.moo.foo.lay.f will be lib->moo->foo->lay->f)

		// Find outermost parent
		outerMostParent := node
		for {
			parent := outerMostParent.Parent()
			if parent != nil {
				if parent.GrammarName() == "access" {
					outerMostParent = parent
					continue
				}
			}
			break
		}

		if outerMostParent.GrammarName() == "access" {
			node = outerMostParent
		}

		identString := node.Utf8Text(content)
		// Get Node range and find smallest scope that contains it
		identStart := node.StartPosition()
		identEnd := node.EndPosition()

		identRange := transport.Range{
			Start: transport.Position{Line: uint32(identStart.Row), Character: uint32(identStart.Column)},
			End:   transport.Position{Line: uint32(identEnd.Row), Character: uint32(identEnd.Column)},
		}

		lowestScope := FindLowestScopeContainingRange(scope, identRange)

		return identString, lowestScope
	}

	return "", nil
}

func FindSymbolScopeAtOffset(content []byte, scope *Scope, offset uint, encoding string) (string, *Scope) {
	// Manual version of FindSymbolScope that doesn't use tree-sitter to find the identifier at the given offset
	i, j := offset, offset
	for {
		if i == 0 || j == uint(len(content)-1) {
			break
		}
		if unicode.IsLetter(rune(content[i])) || unicode.IsDigit(rune(content[i])) || content[i] == '.' {
			i--
		}
		if unicode.IsLetter(rune(content[j])) || unicode.IsDigit(rune(content[j])) || content[j] == '.' {
			j++
		} else {
			break
		}
	}
	ident := content[i:j]
	if string(ident) == "" {
		// Trying to go back from offset to find identifier
		i--
		for {
			if i <= 0 {
				break
			}
			if unicode.IsLetter(rune(content[i])) || unicode.IsDigit(rune(content[i])) || content[i] == '.' {
				i--
			} else {
				break
			}
		}
		ident = content[i+1 : j]
	}

	logging.Logger.Info("Found identifier at offset", "ident", string(ident), "start", i+1, "end", j, "offset", offset)
	start, err := OffsetToPosition(i, string(content), encoding)
	end, err := OffsetToPosition(j, string(content), encoding)
	if err != nil {
		return "", nil
	}
	identRange := transport.Range{
		Start: start,
		End:   end,
	}
	lowestScope := FindLowestScopeContainingRange(scope, identRange)
	return string(ident), lowestScope
}

func FindLowestScopeContainingRange(scope *Scope, identRange transport.Range) *Scope {
	if scope != nil {
		//		logging.Logger.Info("Scope children", "length", len(scope.Children))
		for _, childScope := range scope.Children {
			//			logging.Logger.Info("Current child scope", "no", i)
			//			logging.Logger.Info("Looking in child scope to find lowest scope", "current", scope.Range, "child", childScope.Range, "target", identRange)
			//			logging.Logger.Info("What is parent scope ?", "scope", scope.Symbols[0])
			if childScope != nil {
				if RangeContains(childScope.Range, identRange) {
					//					logging.Logger.Info("Scope contains identifier", "scope", childScope.Range, "ident", identRange)
					return FindLowestScopeContainingRange(childScope, identRange)
				} else {
					//					logging.Logger.Info("Parent scope does not contain child scope", "parent", scope.Range, "child", childScope.Range)
				}
			}
		}
	}
	//	logging.Logger.Info("Returning current scope", "scope", scope.Range)
	return scope
}

func RangeContains(parent transport.Range, child transport.Range) bool {
	// OLD
	// below := parent.Start.Line <= child.Start.Line && parent.Start.Character <= child.Start.Character
	// above := child.End.Line <= parent.End.Line && child.End.Character <= parent.End.Character
	// return above && below

	// NEW
	// Failed cases: Parent: {{0, 0}, {2, 0}}, Child: {{1,0}, {1,17}}
	start_is_between := (parent.Start.Line < child.Start.Line) ||
		(parent.Start.Line == child.Start.Line && parent.Start.Character <= child.Start.Character)
	end_is_between := (parent.End.Line > child.End.Line) ||
		(parent.End.Line == child.End.Line && parent.End.Character >= child.End.Character)

	return start_is_between && end_is_between
}
