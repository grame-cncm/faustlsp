package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"faustlsp/logging"
	"faustlsp/transport"
	"fmt"
)

// TODO: Have a type for request ID

type ServerState int 

const (
	Created       = iota
	Initializing
	Initialized
	Shutdown
	Exit
)

// Main Server Struct
type Server struct {
	// TODO: workspaceFolders, diagnosticsBundle, mutex
	// TODO: request id counter so that we can send our own requests

	Status ServerState

	// Allows to add other transportation methods in the future
	// possible values: stdin | socket
	Transport transport.Transport	
}

// Initialize Server
func (s *Server) Init(transp transport.TransportMethod){
	s.Status = Created
	s.Transport.Init(transport.Server, transp)
	return;
}

// Might be pointless ?
// Wanted a way to handle both cancel and ending gracefully from the loop go routine while handling or logging possible errors
func (s *Server) Run(ctx context.Context) {
	end := make(chan error, 1)
	go s.Loop(ctx, end)
	select {
	case err := <-end:
		if err != nil {
			errormsg := "Ending because of error ("+err.Error()+")"
			logging.Logger.Println(errormsg)
			fmt.Println(errormsg)
		} else {
			logging.Logger.Println("LSP Successfully Exited")
		}
	case <- ctx.Done():
		logging.Logger.Println("Canceling Main Loop")
		return
	}
}

// The central LSP server loop
func (s *Server) Loop(ctx context.Context, end chan<- error){
	var err error
	var msg []byte

	// LSP Server Main Loop
	for s.Status != Shutdown && !s.Transport.Closed && err == nil{
		var method string
		// If parent cancels, make sure to stop
		select {
		case <-ctx.Done():
			break
		default:
		}

		// Read one JSON RPC Message
		msg, err = s.Transport.Read()

		// Parse JSON RPC Message here and get method
		method, err = transport.GetMethod(msg)
		if len(method) == 0 {break}
		if err != nil { break }

		logging.Logger.Println("Got Method: "+method)
		// Validate Message (error if the client shouldn't be sending that method)
		err = s.ValidateMethod(method)
		if err != nil {break}
		
		// Dispatch to Method Handler
		go s.HandleMethod(method, msg)
		
		// Dirty way to handle server ending for now
		// Goal is to handle LSP exit message and set s.Status to Exit and end
		if string(msg) == "Content-Length: 3\r\n\r\nend" {
			ctx.Done()
			break
		}
	}
	if err == nil && s.Transport.Closed {err = errors.New("Stream Closed: Got EOF")}
	s.Transport.Close()
	end <- err
}


// Validates if current method is valid given current server State
// TODO: Handle all server states
func (s *Server) ValidateMethod(method string) error{
	switch s.Status {
	case Created:
		if method != "initialize"{
			return errors.New("Server not started, but received "+method)
		}
	case Shutdown:
		if method != "initialize"{
			return errors.New("Server not started, but received "+method)
		}
	}
	return nil
}

// Main Handle Method
func(s *Server) HandleMethod(method string, message []byte){
	// TODO: Receive only content, no Header
	_, content, _:= bytes.Cut(message, []byte{'\r','\n','\r','\n'})
	switch method {
	// TODO: Make sure to group based on Request or Notification message
	// TODO: Send appropriate error if not possible to handle request
	case "initialize":
		var m transport.RequestMessage
		json.Unmarshal(content, &m) 

		// Send Request Message to appropriate Handler
		handler := requestHandlers[method]
		resp, err := handler(s,m.ID,m.Params)
		if err != nil {
			logging.Logger.Println(err)
			return
		}

		if (len(resp) != 0){
			logging.Logger.Printf("Writing %s\n",resp)
			err = s.Transport.Write(resp)
			if err != nil {
				logging.Logger.Println(err)
			}
		}
	}
}


// Map from method to method handler for request methods
var requestHandlers = map[string]func(s *Server, id interface{}, params json.RawMessage) (json.RawMessage, error){
	"initialize": Initialize,
}


// Initialize Handler
func Initialize(s *Server, id interface{}, par json.RawMessage) (json.RawMessage, error){
	// TODO: Error Handling
	
	s.Status = Initializing
	logging.Logger.Printf("Handling Initialize(id: %v)",id)
	var params transport.InitializeParams
	json.Unmarshal(par, &params)
	logging.Logger.Println(params)

	// TODO: Choose ServerCapabilities based on ClientCapabilities
	// Server Capabilities
	var result transport.InitializeResult = transport.InitializeResult{
		Capabilities: transport.ServerCapabilities{
			TextDocumentSync: transport.Incremental,
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
		ID: id,
		Result: resultBytes,
	}
	msg, err := json.Marshal(resp)
	return msg, err
}
