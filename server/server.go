package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/parser"
	"github.com/carn181/faustlsp/transport"
	"github.com/carn181/faustlsp/util"
)

// TODO: Have a type for request ID

type ServerState int

const (
	Created = iota
	Initializing
	Running
	Shutdown
	Exit
	ExitError
)

// Main Server Struct
type Server struct {
	// TODO: workspaceFolders, diagnosticsBundle, mutex
	// TODO: request id counter so that we can send our own requests
	// Capabalities
	Capabilities transport.ServerCapabilities

	// Workspace and Files are different because in future should allow having multiple workspaces while having one main File Store, but both have to be synchronized on each document Change
	Workspace Workspace
	Files     Files
	Symbols   SymbolStore

	Status ServerState
	mu     sync.Mutex

	// Allows to add other transportation methods in the future
	// possible values: stdin | socket
	Transport transport.Transport

	// Request Id Counter for new requests
	reqIdCtr int

	// Temporary Directory where we replicate workspace for diagnostics
	tempDir util.Path

	// Diagnostic Channel
	diagChan chan transport.PublishDiagnosticsParams
}

// Initialize Server
func (s *Server) Init(transp transport.TransportMethod) {
	s.Status = Created
	s.Transport.Init(transport.Server, transp)
	parser.Init()
	s.Symbols.Init()

	// Create Temporary Directory
	faustTemp := filepath.Join(os.TempDir(), "faustlsp") // No need to create $TEMPDIR/faustlsp as logging should create it
	temp_dir, err := os.MkdirTemp(faustTemp, "faustlsp-")
	if err != nil {
		logging.Logger.Error("Couldn't create temp dir", "error", err)
		return
	} else {
		logging.Logger.Info("Created Temp Directory", "path", temp_dir)
	}
	s.tempDir = temp_dir
}

// Might be pointless ?
// Wanted a way to handle both cancel and ending gracefully from the loop go routine while handling or logging possible errors
func (s *Server) Run(ctx context.Context) error {
	var returnError error
	end := make(chan error, 1)
	go s.Loop(ctx, end)
	select {
	case err := <-end:
		if err != nil {
			errormsg := "Ending because of error (" + err.Error() + ")"
			logging.Logger.Info(errormsg)
			fmt.Println(errormsg)
			returnError = errors.New(err.Error())
		} else {
			logging.Logger.Info("LSP Successfully Exited")
		}
	case <-ctx.Done():
		logging.Logger.Info("Canceling Main Loop")
	}

	// TODO: Have a proper cleanup function here
	parser.Close()
	os.RemoveAll(s.tempDir)
	return returnError
}

// The central LSP server loop
func (s *Server) Loop(ctx context.Context, end chan<- error) {
	var err error
	var msg []byte
	var method string

	// LSP Server Main Loop
	for s.Status != Exit && s.Status != ExitError && !s.Transport.Closed && err == nil {
		// If parent cancels, make sure to stop
		select {
		case <-ctx.Done():
			break
		default:
		}

		// Read one JSON RPC Message
		logging.Logger.Debug("Reading")
		msg, err = s.Transport.Read()
		if err != nil {
			logging.Logger.Error("Scanning error", "error", err)
		}

		// Parse JSON RPC Message here and get method
		method, err = transport.GetMethod(msg)
		if len(method) == 0 {
			break
		}
		if err != nil {
			logging.Logger.Error("Parsing error", "error", err)
			break
		}

		logging.Logger.Debug("Got Method: " + method)

		// Validate Message (error if the client shouldn't be sending that method)
		err = s.ValidateMethod(method)
		if err != nil {
			break
		}

		// Dispatch to Method Handler

		// Might add other methods here
		// If exit or shutdown, don't run concurrently and change state for loop to end
		if method != "exit" && method != "shutdown" {
			go s.HandleMethod(ctx, method, msg)
		} else {
			s.HandleMethod(ctx, method, msg)
		}
	}
	if s.Status == ExitError {
		err = errors.New("exiting ungracefully")
		end <- err
	} else if s.Status == Exit {
		end <- nil
		return
	}
	if err == nil && s.Transport.Closed {
		err = errors.New("stream closed: got EOF")
	} else {
		s.Transport.Close()
	}
	end <- err
}

// Validates if current method is valid given current server State
// TODO: Handle all server states
func (s *Server) ValidateMethod(method string) error {
	switch s.Status {
	case Created:
		if method != "initialize" {
			return errors.New("Server not started, but received " + method)
		}
	case Shutdown:
		if method != "exit" {
			return errors.New("Can only exit" + method)
		}
	}
	return nil
}

// Main Handle Method
func (s *Server) HandleMethod(ctx context.Context, method string, content []byte) {
	// TODO: Receive only content, no Header
	handler, ok := requestHandlers[method]
	if ok {
		var m transport.RequestMessage
		json.Unmarshal(content, &m)
		logging.Logger.Debug("Request ID", "type", reflect.TypeOf(m.ID), "value", m.ID)
		if reflect.TypeOf(m.ID).String() == "float64" {
			s.reqIdCtr = int(m.ID.(float64) + 1)
		}

		// Main handle method for request and get response
		resp, err := handler(ctx, s, m.Params)

		var responseError *transport.ResponseError
		if err != nil {
			responseError = &transport.ResponseError{
				Code:    int(transport.InternalError),
				Message: err.Error(),
			}
		}
		err = s.Transport.WriteResponse(m.ID, resp, responseError)
		if err != nil {
			logging.Logger.Warn(err.Error())
			return
		}

		return
	}
	handler2, ok := notificationHandlers[method]
	if ok {
		var m transport.NotificationMessage
		json.Unmarshal(content, &m)

		// Send Request Message to appropriate Handler
		err := handler2(ctx, s, m.Params)
		if err != nil {
			logging.Logger.Warn(err.Error())
			return
		}
	}
	return
}

// Map from method to method handler for request methods
var requestHandlers = map[string]func(context.Context, *Server, json.RawMessage) (json.RawMessage, error){
	"initialize":                  Initialize,
	"textDocument/documentSymbol": TextDocumentSymbol,
	"textDocument/formatting":     Formatting,
	//	"textDocument/definition":     Definition,
	"shutdown": ShutdownEnd,
}

// Map from method to method handler for request methods
var notificationHandlers = map[string]func(context.Context, *Server, json.RawMessage) error{
	"initialized":            Initialized,
	"textDocument/didOpen":   TextDocumentOpen,
	"textDocument/didChange": TextDocumentChangeIncremental,
	"textDocument/didClose":  TextDocumentClose,
	// The save action of textDocument/didSave should be handled by our watcher to our store, so no need to handle
	"exit": ExitEnd,
}

func TextDocumentSymbol(ctx context.Context, s *Server, par json.RawMessage) (json.RawMessage, error) {
	var params transport.DocumentSymbolParams
	json.Unmarshal(par, &params)

	fileURI := params.TextDocument.URI
	path, err := util.URI2path(string(fileURI))
	if err != nil {
		return []byte{}, err
	}
	f, ok := s.Files.Get(path)
	if !ok {
		return []byte{}, fmt.Errorf("trying to get symbols from non-existent path: %s", path)
	}
	result := f.DocumentSymbols()

	resultBytes, err := json.Marshal(result)

	return resultBytes, err
}
