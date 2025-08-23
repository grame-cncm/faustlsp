package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/parser"
	"github.com/carn181/faustlsp/transport"
	"github.com/carn181/faustlsp/util"
)

func GetDefinition(ctx context.Context, s *Server, par json.RawMessage) (json.RawMessage, error) {
	// TODO: Work on this function
	var params transport.DefinitionParams
	json.Unmarshal(par, &params)

	logging.Logger.Info("Goto Definition Request", "params", params)
	path, err := util.URI2path(string(params.TextDocument.URI))
	if err != nil {
		logging.Logger.Error("Uri2path error", "error", err)
		return []byte{}, err
	}

	f, ok := s.Files.GetFromPath(path)
	if !ok {
		logging.Logger.Error("File should've been in server file store", "path", path)
	}

	offset, err := PositionToOffset(params.Position, string(f.Content), string(s.Files.encoding))
	if err != nil {
		return []byte{}, err
	}

	ident, scope := FindSymbolScope(f.Content, f.Scope, offset)

	logging.Logger.Info("Got symbol at Location", "symbol", ident, "scope_exists", f.Scope != nil)

	if ident == "" {
		// Couldn't find symbol to lookup
		return []byte("null"), nil
	}

	var loc Location
	identSplit := strings.Split(ident, ".")

	if len(identSplit) > 1 {
		logging.Logger.Info("Resolving library symbol", "symbol", identSplit)
		for i := range len(identSplit) - 1 {
			libIdent := identSplit[i]

			// Resolve as Environment
			sym, err := FindEnvironmentIdent(libIdent, scope, &s.Store)
			logging.Logger.Info("Resolved environment", "env", libIdent, "sym", sym.Ident, "loc", sym.Loc)
			if err == nil {
				loc = sym.Loc
				scope = sym.Scope
				continue
			}

			// Resolve as Library if not resolved as environment
			file, err := FindLibraryIdent(libIdent, scope, &s.Store)
			if err != nil {
				break
			}
			logging.Logger.Info("Resolved library environment", "env", libIdent, "location", file)
			f, ok := s.Store.Files.GetFromPath(file)
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

	loc, err = FindDefinition(ident, scope, &s.Store)

	logging.Logger.Info("Got definition as", "location", loc, "error", err)
	if err == nil {
		fileLocation := transport.Location{
			URI:   transport.DocumentURI(util.Path2URI(loc.File)),
			Range: loc.Range,
		}
		result, err := json.Marshal(fileLocation)
		if err == nil {
			return result, nil
		}
	}

	return []byte("null"), nil
}

func Hover(ctx context.Context, s *Server, par json.RawMessage) (json.RawMessage, error) {
	// TODO: Work on this function
	var params transport.HoverParams
	json.Unmarshal(par, &params)

	logging.Logger.Info("Hover Request", "params", params)
	path, err := util.URI2path(string(params.TextDocument.URI))
	if err != nil {
		logging.Logger.Error("Uri2path error", "error", err)
		return []byte{}, err
	}

	f, ok := s.Files.GetFromPath(path)
	if !ok {
		logging.Logger.Error("File should've been in server file store", "path", path)
	}

	offset, err := PositionToOffset(params.Position, string(f.Content), string(s.Files.encoding))
	if err != nil {
		return []byte{}, err
	}

	ident, scope := FindSymbolScope(f.Content, f.Scope, offset)

	logging.Logger.Info("Got symbol at Location", "symbol", ident, "scope_exists", f.Scope != nil)

	if ident == "" {
		// Couldn't find symbol to lookup
		return []byte("null"), nil
	}

	identSplit := strings.Split(ident, ".")

	if len(identSplit) > 1 {
		logging.Logger.Info("Resolving library symbol", "symbol", identSplit)
		for i := range len(identSplit) - 1 {
			libIdent := identSplit[i]

			// Resolve as Environment
			sym, err := FindEnvironmentIdent(libIdent, scope, &s.Store)
			logging.Logger.Info("Resolved environment", "env", libIdent, "sym", sym.Ident, "loc", sym.Loc)
			if err == nil {
				scope = sym.Scope
				continue
			}

			// Resolve as Library if not resolved as environment
			file, err := FindLibraryIdent(libIdent, scope, &s.Store)
			if err != nil {
				break
			}
			logging.Logger.Info("Resolved library environment", "env", libIdent, "location", file)
			f, ok := s.Store.Files.GetFromPath(file)
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

	docs, err := FindDocs(ident, scope, &s.Store)

	logging.Logger.Info("Got docs as", "documentation", docs, "error", err)
	if err == nil {
		docsResp := transport.Hover{
			Contents: transport.MarkupContent{
				Kind:  transport.Markdown,
				Value: docs,
			},
		}
		result, err := json.Marshal(docsResp)
		if err == nil {
			return result, nil
		}
	}

	return []byte("null"), nil
}

func GetReferences(ctx context.Context, s *Server, par json.RawMessage) (json.RawMessage, error) {
	// TODO: Work on this function
	var params transport.DefinitionParams
	json.Unmarshal(par, &params)

	logging.Logger.Info("Goto Definition Request", "params", params)
	path, err := util.URI2path(string(params.TextDocument.URI))
	if err != nil {
		logging.Logger.Error("Uri2path error", "error", err)
		return []byte{}, err
	}

	f, ok := s.Files.GetFromPath(path)
	if !ok {
		logging.Logger.Error("File should've been in server file store", "path", path)
	}

	offset, err := PositionToOffset(params.Position, string(f.Content), string(s.Files.encoding))
	if err != nil {
		return []byte{}, err
	}

	ident, scope := FindSymbolScope(f.Content, f.Scope, offset)

	logging.Logger.Info("Got symbol at Location", "symbol", ident, "scope", f.Scope == nil)

	if ident == "" {
		// Couldn't find symbol to lookup
		return []byte("null"), nil
	}

	var loc Location
	identSplit := strings.Split(ident, ".")
	if len(identSplit) > 1 {
		logging.Logger.Info("Resolving library symbol", "symbol", identSplit)
		for _, libIdent := range identSplit {
			// Resolve as Environment
			sym, err := FindEnvironmentIdent(ident, scope, &s.Store)
			logging.Logger.Info("Resolved environment", "env", libIdent, "sym", sym.Ident, "loc", sym.Loc)
			if err == nil {
				loc = sym.Loc
				scope = sym.Scope
				continue
			}

			// Resolve as Library if not resolved as environment
			file, err := FindLibraryIdent(libIdent, scope, &s.Store)
			if err != nil {
				break
			}
			logging.Logger.Info("Resolved library environment", "env", libIdent, "location", file)
			f, ok := s.Store.Files.GetFromPath(file)
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

	loc, err = FindDefinition(ident, scope, &s.Store)

	logging.Logger.Info("Got definition as", "location", loc, "error", err)
	if err == nil {
		// Find references using location
		// FindReferences(loc, store) (Location[], error)
		// Parse file tree for references (parse new tree and query pure identifiers)
		// Go through scopes and check their expressions for references if it contains same symbol definition and remove from this file tree
		// Do same for all importers till no other importers (avoid cycles too)
		//		startFile := loc.File
		//		importers := s.Store.Dependencies.GetImporters(startFile)

		fileLocation := transport.Location{
			URI:   transport.DocumentURI(util.Path2URI(loc.File)),
			Range: loc.Range,
		}
		result, err := json.Marshal(fileLocation)
		if err == nil {
			return result, nil
		}
	}

	return []byte("null"), nil
}

func RefQuery(ident string) string {
	return fmt.Sprintf(`
((identifier) @l
	(#eq? @l %s)
)`, ident)
}

func GetRefsForFile(ident string, path util.Path, store *Store) []Location {
	f, ok := store.Files.GetFromPath(path)
	if !ok {
		return []Location{}
	}

	locations := []Location{}

	// Parse through Scope
	tree := parser.ParseTree(f.Content)
	defer tree.Close()
	results := parser.GetQueryMatches(RefQuery(ident), f.Content, tree)

	totalRefs := make(map[transport.Range]struct{})
	for _, result := range results.Results {
		for _, refs := range result {
			totalRefs[ToRange(&refs)] = struct{}{}
		}
	}

	//	CleanUpRefs(ident, , currentRefs map[transport.Range]struct{}, content []byte)

	return locations
}

func CleanUpRefs(ident string, symbol *Symbol, currentRefs map[transport.Range]struct{}, content []byte) {
	// 1) Check if definition of same identifier exists
	defined := false
	for _, child := range symbol.Scope.Symbols {
		if child.Ident == ident {
			defined = true
		}
	}

	if defined {
		results := parser.GetQueryMatchesFromNode(RefQuery(ident), content, symbol.Expr)
		for _, resultType := range results.Results {
			for _, result := range resultType {
				delete(currentRefs, ToRange(&result))
			}
		}
	}

	for _, child := range symbol.Scope.Symbols {
		if child.Scope != nil {
			CleanUpRefs(ident, child, currentRefs, content)
		}
	}
}

// Parse current scope, add to found references list.
// Iterate through child scope recursively, remove from references list if found in child scope and scope has definition of same reference
