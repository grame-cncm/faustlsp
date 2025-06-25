package server

import (
	"context"
	"encoding/json"
	"os"

	"faustlsp/logging"
	"faustlsp/transport"
	"faustlsp/util"
)

// Initialize Handler
func Initialize(ctx context.Context, s *Server, id any, par json.RawMessage) (json.RawMessage, error) {
	// TODO: Error Handling

	s.Status = Initializing
	logging.Logger.Printf("Handling Initialize(id: %v)", id)
	var params transport.InitializeParams
	json.Unmarshal(par, &params)
	logging.Logger.Println(string(par))

	// TODO: Choose ServerCapabilities based on ClientCapabilities
	// Server Capabilities

	// Don't select UTF-8, select UTF-32 and UTF-16 only
	var positionEncoding transport.PositionEncodingKind
	if params.Capabilities.General.PositionEncodings[0] == "utf-16" {
		positionEncoding = transport.UTF16
	} else if params.Capabilities.General.PositionEncodings[0] == "utf-32" {
		positionEncoding = transport.UTF32
	} else {
		positionEncoding = transport.UTF16
	}
	var result transport.InitializeResult = transport.InitializeResult{
		Capabilities: transport.ServerCapabilities{
			// TODO: Implement Incremental Changes for better synchronization
			DocumentSymbolProvider: &transport.Or_ServerCapabilities_documentSymbolProvider{Value: true},
			PositionEncoding:       &positionEncoding,
			TextDocumentSync:       transport.Incremental,
			Workspace: &transport.WorkspaceOptions{
				WorkspaceFolders: &transport.WorkspaceFolders5Gn{
					Supported:           true,
					ChangeNotifications: "ws",
				},
			},
		},
		ServerInfo: &transport.ServerInfo{Name: "faust-lsp", Version: "0.0.1"},
	}
	s.Capabilities = result.Capabilities

	rootPath, _ := util.Uri2path(string(params.RootURI))
	logging.Logger.Printf("Workspace: %v\n", rootPath)
	s.Workspace.Root = rootPath

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

// Initialized Handler
func Initialized(ctx context.Context, s *Server, par json.RawMessage) error {

	s.Status = Running
	go s.GenerateDiagnostics()
	s.Files.Init(ctx, *s.Capabilities.PositionEncoding)
	s.Workspace.Init(ctx, s)
	logging.Logger.Println("Handling Initialized with diagnostics")
	logging.Logger.Println("Started Diagnostic Handler")
	// Send WorkspaceFolders Request
	// TODO: Do this only if server-client agreed on workspacefolders
	//	err := s.Transport.WriteRequest(s.reqIdCtr,"workspace/workspaceFolders", []byte{})
	//	if err != nil {
	//		logging.Logger.Fatal(err)
	//	}
	//	s.reqIdCtr+=1
	return nil
}

// Shutdown Handler
func ShutdownEnd(ctx context.Context, s *Server, id any, par json.RawMessage) (json.RawMessage, error) {
	s.Status = Shutdown
	var result = transport.ResponseMessage{
		Message: transport.Message{Jsonrpc: "2.0"},
		ID:      id,
		Result:  []byte("{}"),
	}
	// Some Clients end the server right after sending shutdown like emacs lsp-mode
	// Remove Temp Dir just in case
	os.RemoveAll(s.tempDir)

	content, err := json.Marshal(result)
	return content, err
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
