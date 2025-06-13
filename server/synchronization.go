package server

import (
	"context"
	"encoding/json"
	"faustlsp/logging"
	"faustlsp/transport"
	"faustlsp/util"
)

func TextDocumentOpen(ctx context.Context, s *Server, par json.RawMessage) error {
	var params transport.DidOpenTextDocumentParams
	json.Unmarshal(par, &params)

	fileURI := params.TextDocument.URI

	// Open File
	s.Files.OpenFromURI(string(fileURI))

	logging.Logger.Printf("Opening File %s\n", string(fileURI))
	logging.Logger.Printf("Current Files: %v\n", s.Files)
	return nil
}

func TextDocumentChange(ctx context.Context, s *Server, par json.RawMessage) error {
	// TODO: Check if server-client agreed to incremental and do incremental change, else do full
	// TODO: Handle incremental changes. Currently only full

	var params transport.DidChangeTextDocumentParams
	json.Unmarshal(par, &params)

	fileURI := params.TextDocument.URI

	// Apply Full TextDocumentChange
	path, err := util.Uri2path(string(fileURI))
	if err != nil {
		return err
	}
	for _, change := range params.ContentChanges {
		s.Files.ModifyFull(path, change.Text)
	}
	logging.Logger.Printf("Modified File %s\n", string(fileURI))
	logging.Logger.Printf("Current Files: %s\n", s.Files)
	return nil
}

func TextDocumentClose(ctx context.Context, s *Server, par json.RawMessage) error {
	var params transport.DidCloseTextDocumentParams
	json.Unmarshal(par, &params)

	fileURI := params.TextDocument.URI

	s.Files.CloseFromURI(util.Path(params.TextDocument.URI))

	logging.Logger.Printf("Closed File %s\n", string(fileURI))
	logging.Logger.Printf("Current Files: %s\n", s.Files)
	return nil
}
