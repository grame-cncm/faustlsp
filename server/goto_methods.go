package server

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/carn181/faustlsp/logging"
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

	logging.Logger.Info("Got symbol at Location", "symbol", ident, "scope", f.Scope == nil)

	if ident == "" {
		// Couldn't find symbol to lookup
		return []byte{}, nil
	}

	var loc Location
	identSplit := strings.Split(ident, ".")
	if len(identSplit) > 1 {
		logging.Logger.Info("Resolving library symbol", "symbol", identSplit)
		for _, libIdent := range identSplit {
			loc, err := FindEnvironmentIdent(libIdent, scope, &s.Store)
			if err != nil {
				break
			}
			logging.Logger.Info("Resolved library environment", "env", libIdent, "location", loc)
			f, ok := s.Store.Files.GetFromPath(loc)
			if ok {
				f.mu.RLock()
				logging.Logger.Info("Setting New Scope to", "path", loc)
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

	return []byte{}, nil
}
