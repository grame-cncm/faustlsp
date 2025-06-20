package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"faustlsp/logging"
	"faustlsp/parser"
	"faustlsp/transport"
	"faustlsp/util"
	"fmt"
	"os"
	"reflect"
	"sync"
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

	Status ServerState
	mu     sync.Mutex

	// Allows to add other transportation methods in the future
	// possible values: stdin | socket
	Transport transport.Transport

	// Request Id Counter for new requests
	reqIdCtr int

	// Temporary Directory where we replicate workspace for diagnostics
	tempDir util.Path
}

// Initialize Server
func (s *Server) Init(transp transport.TransportMethod) {
	s.Status = Created
	s.Transport.Init(transport.Server, transp)
	parser.Init()

	// Create Temporary Directory
	os_temp := os.TempDir()
	temp_dir, err := os.MkdirTemp(os_temp, "faustlsp-")
	if err != nil {
		logging.Logger.Fatalf("Couldn't create temp dir: %s\n", err)
		return
	} else {
		logging.Logger.Printf("Created Temp Directory at %s\n", temp_dir)
	}
	s.tempDir = temp_dir
	return
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
			logging.Logger.Println(errormsg)
			fmt.Println(errormsg)
			returnError = errors.New(err.Error())
		} else {
			logging.Logger.Println("LSP Successfully Exited")
		}
	case <-ctx.Done():
		logging.Logger.Println("Canceling Main Loop")
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
		logging.Logger.Println("Reading")
		msg, err = s.Transport.Read()

		// Parse JSON RPC Message here and get method
		method, err = transport.GetMethod(msg)
		if len(method) == 0 {
			break
		}
		if err != nil {
			break
		}

		logging.Logger.Println("Got Method: " + method)

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
		err = errors.New("Exiting Ungracefully")
		end <- err
	} else if s.Status == Exit {
		end <- nil
		return
	}
	if err == nil && s.Transport.Closed {
		err = errors.New("Stream Closed: Got EOF")
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
func (s *Server) HandleMethod(ctx context.Context, method string, message []byte) {
	// TODO: Receive only content, no Header
	_, content, _ := bytes.Cut(message, []byte{'\r', '\n', '\r', '\n'})

	handler, ok := requestHandlers[method]
	if ok {
		var m transport.RequestMessage
		json.Unmarshal(content, &m)
		logging.Logger.Printf("Got type of ID: %s\n", reflect.TypeOf(m.ID))
		if reflect.TypeOf(m.ID).String() == "float64" {
			s.reqIdCtr = int(m.ID.(float64) + 1)
		}
		resp, err := handler(ctx, s, m.ID, m.Params)
		if err != nil {
			logging.Logger.Println(err)
			return
		}

		if len(resp) != 0 {
			logging.Logger.Printf("Writing %s\n", resp)
			err = s.Transport.Write(resp)
			if err != nil {
				logging.Logger.Println(err)
			}
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
			logging.Logger.Println(err)
			return
		}
	}
	return
}

// Map from method to method handler for request methods
var requestHandlers = map[string]func(context.Context, *Server, interface{}, json.RawMessage) (json.RawMessage, error){
	"initialize":                  Initialize,
	"textDocument/documentSymbol": TextDocumentSymbol,
	"shutdown":                    ShutdownEnd,
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

func TextDocumentSymbol(ctx context.Context, s *Server, id interface{}, par json.RawMessage) (json.RawMessage, error) {
	var params transport.DocumentSymbolParams
	json.Unmarshal(par, &params)

	fileURI := params.TextDocument.URI
	path, err := util.Uri2path(string(fileURI))
	if err != nil {
		return []byte{}, err
	}
	f, ok := s.Files.Get(path)
	if !ok {
		return []byte{}, fmt.Errorf("Trying to get symbols from non-exist %s\n", path)
	}
	result := f.DocumentSymbols()

	resultBytes, err := json.Marshal(result)
	//	logging.Logger.Println(string(resultBytes))
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
