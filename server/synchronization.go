package server

import (
	"context"
	"encoding/json"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/transport"
	"github.com/carn181/faustlsp/util"
)

type TDChangeType int

const (
	TDOpen = iota
	TDChange
	TDClose
)

type TDEvent struct {
	Type TDChangeType
	Path util.Path
}

func TextDocumentOpen(ctx context.Context, s *Server, par json.RawMessage) error {
	var params transport.DidOpenTextDocumentParams
	json.Unmarshal(par, &params)

	fileURI := params.TextDocument.URI

	// Open File
	s.Workspace.EditorOpenFile(util.URI(fileURI), &s.Files)

	logging.Logger.Info("Opening File", "uri", string(fileURI))
	f, _ := s.Files.GetFromURI(util.URI(fileURI))

	f.mu.RLock()
	logging.Logger.Info("Current File", "content", f.Content)

	s.Workspace.TDEvents <- TDEvent{Type: TDOpen, Path: f.Handle.Path}

	f.mu.RUnlock()
	return nil
}

func TextDocumentChangeFull(ctx context.Context, s *Server, par json.RawMessage) error {
	var params transport.DidChangeTextDocumentParams
	json.Unmarshal(par, &params)

	fileURI := params.TextDocument.URI

	path, err := util.URI2path(string(fileURI))
	if err != nil {
		return err
	}
	for _, change := range params.ContentChanges {
		s.Files.ModifyFull(path, change.Text)
	}
	s.Workspace.TDEvents <- TDEvent{Type: TDChange, Path: path}

	logging.Logger.Info("Modified File", "fileURI", string(fileURI))
	return nil
}

func TextDocumentChangeIncremental(ctx context.Context, s *Server, par json.RawMessage) error {
	var params transport.DidChangeTextDocumentParams
	json.Unmarshal(par, &params)
	logging.Logger.Info("TextDocumentChangeIncremental", "params", string(par))
	fileURI := params.TextDocument.URI

	path, err := util.URI2path(string(fileURI))
	if err != nil {
		return err
	}
	for _, change := range params.ContentChanges {
		s.Files.ModifyIncremental(path, *change.Range, change.Text)
	}

	s.Workspace.TDEvents <- TDEvent{Type: TDChange, Path: path}

	return nil
}

func TextDocumentClose(ctx context.Context, s *Server, par json.RawMessage) error {
	var params transport.DidCloseTextDocumentParams
	json.Unmarshal(par, &params)

	fileURI := params.TextDocument.URI

	s.Files.CloseFromURI(util.Path(params.TextDocument.URI))

	path, err := util.URI2path(string(fileURI))
	logging.Logger.Error("Got error when getting path from URI", "error", err)
	s.Workspace.TDEvents <- TDEvent{Type: TDClose, Path: path}

	logging.Logger.Info("Closed File", "uri", string(fileURI))
	//	logging.Logger.Printf("Current Files: %s\n", s.Files)
	return nil
}
