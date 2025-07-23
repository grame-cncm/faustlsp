package server

import (
	"context"
	"encoding/json"

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

	_, err = PositionToOffset(params.Position, string(f.Content), string(s.Files.encoding))
	if err != nil {
		return []byte{}, err
	}

	return []byte{}, err
}
