package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/transport"
	"github.com/carn181/faustlsp/util"
)

// Initialize Handler
func Initialize(ctx context.Context, s *Server, par json.RawMessage) (json.RawMessage, error) {
	// TODO: Error Handling

	s.Status = Initializing
	var params transport.InitializeParams
	json.Unmarshal(par, &params)
	logging.Logger.Info("Got Initialize Parameters from Client", "params", par)

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
			DocumentFormattingProvider: &transport.Or_ServerCapabilities_documentFormattingProvider{Value: true},
			DefinitionProvider:         &transport.Or_ServerCapabilities_definitionProvider{Value: true},
			HoverProvider:              &transport.Or_ServerCapabilities_hoverProvider{Value: true},
			CompletionProvider: &transport.CompletionOptions{
				TriggerCharacters: []string{"."},
			},
		},
		ServerInfo: &transport.ServerInfo{Name: "faust-lsp", Version: "0.0.1"},
	}
	s.Capabilities = result.Capabilities

	rootPath, _ := util.URI2path(string(params.RootURI))
	logging.Logger.Info("Got workspace", "workspace", rootPath)
	s.Workspace.Root = rootPath

	resultBytes, err := json.Marshal(result)
	if err != nil {
		return []byte{}, nil
	}
	return resultBytes, err
}

// Initialized Handler
func Initialized(ctx context.Context, s *Server, par json.RawMessage) error {

	s.Status = Running
	go s.GenerateDiagnostics()
	s.Files.Init(ctx, *s.Capabilities.PositionEncoding)
	s.Store.Files = &s.Files
	s.Store.Dependencies = NewDependencyGraph()
	s.Store.Cache = make(map[[sha256.Size]byte]*Scope)
	s.Workspace.Init(ctx, s)
	logging.Logger.Info("Handling Initialized with diagnostics")
	logging.Logger.Info("Started Diagnostic Handler")
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
func ShutdownEnd(ctx context.Context, s *Server, par json.RawMessage) (json.RawMessage, error) {
	s.Status = Shutdown
	// Some Clients end the server right after sending shutdown like emacs lsp-mode
	// Remove Temp Dir just in case
	os.RemoveAll(s.tempDir)

	content, err := json.Marshal([]byte(""))
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
