package server

import (
	"context"
	"encoding/json"
	"faustlsp/logging"
	"faustlsp/transport"
	"faustlsp/util"
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
	filePath, _ := util.Uri2path(string(fileURI))
	// Open File
	s.Files.OpenFromURI(string(fileURI), s.Workspace.Root, true)

	logging.Logger.Printf("Opening File %s\n", string(fileURI))
	f, _:= s.Files.Get(filePath)
	logging.Logger.Printf("Current File: %s\n",f.Content)

	path, _ := util.Uri2path(string(fileURI))
	s.Workspace.TDEvents <- TDEvent{Type: TDOpen, Path: path}
	return nil
}

func TextDocumentChangeFull(ctx context.Context, s *Server, par json.RawMessage) error {
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
	s.Workspace.TDEvents <- TDEvent{Type: TDChange, Path: path}
	logging.Logger.Printf("Modified File %s\n", string(fileURI))
	//	logging.Logger.Printf("Current Files: %s\n", s.Files)
	return nil
}

func TextDocumentChangeIncremental(ctx context.Context, s *Server, par json.RawMessage) error {
	// TODO: Check if server-client agreed to incremental and do incremental change, else do full
	// TODO: Handle incremental changes. Currently only full

	var params transport.DidChangeTextDocumentParams
	json.Unmarshal(par, &params)
	logging.Logger.Println(string(par))
	fileURI := params.TextDocument.URI

	// Apply Full TextDocumentChange
	path, err := util.Uri2path(string(fileURI))
	if err != nil {
	 	return err
	}
	for _, change := range params.ContentChanges {
	 	s.Files.ModifyIncremental(path, *change.Range, change.Text)
	}
	
	s.Workspace.TDEvents <- TDEvent{Type: TDChange, Path: path}
	//	logging.Logger.Printf("Modified File %s\n", string(fileURI))
	//	logging.Logger.Printf("Current Files: %s\n", s.Files)
	return nil
}

func TextDocumentClose(ctx context.Context, s *Server, par json.RawMessage) error {
	var params transport.DidCloseTextDocumentParams
	json.Unmarshal(par, &params)

	fileURI := params.TextDocument.URI

	s.Files.CloseFromURI(util.Path(params.TextDocument.URI))

	path, _ := util.Uri2path(string(fileURI))
	s.Workspace.TDEvents <- TDEvent{Type: TDClose, Path: path}

	logging.Logger.Printf("Closed File %s\n", string(fileURI))
	//	logging.Logger.Printf("Current Files: %s\n", s.Files)
	return nil
}
