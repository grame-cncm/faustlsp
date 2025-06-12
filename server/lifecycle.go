package server

import (
	"context"
	"encoding/json"

	"faustlsp/logging"
	"faustlsp/transport"
)

// Initialize Handler
func Initialize(ctx context.Context, s *Server, id interface{}, par json.RawMessage) (json.RawMessage, error) {
	// TODO: Error Handling

	s.Status = Initializing
	logging.Logger.Printf("Handling Initialize(id: %v)", id)
	var params transport.InitializeParams
	json.Unmarshal(par, &params)
	logging.Logger.Println(params)

	// TODO: Choose ServerCapabilities based on ClientCapabilities
	// Server Capabilities
	var result transport.InitializeResult = transport.InitializeResult{
		Capabilities: transport.ServerCapabilities{
			// TODO: Implement Incremental Changes for better synchronization
			TextDocumentSync: transport.Full,
			Workspace: &transport.WorkspaceOptions{
				WorkspaceFolders: &transport.WorkspaceFolders5Gn{
					Supported: true,
				},
			},
		},
		ServerInfo: &transport.ServerInfo{Name: "faust-lsp", Version: "0.0.1"},
	}
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return []byte{}, nil
	}
	var resp transport.ResponseMessage = transport.ResponseMessage{
		Message: transport.Message{Jsonrpc: "2.0"},
		ID:      id,
		Result:  resultBytes,
	}
	msg, err := json.Marshal(resp)
	return msg, err
}

// Shutdown Handler
func ShutdownEnd(ctx context.Context, s *Server, id interface{}, par json.RawMessage) (json.RawMessage, error) {
	s.Status = Shutdown
	return []byte{}, nil
}

// Exit Handler
func ExitEnd(ctx context.Context, s *Server, par json.RawMessage) error {
	if s.Status == Shutdown {
		s.Status = Exit
	} else {
		s.Status = ExitError
	}
	return nil
}
