package server

import (
	"context"
	"errors"
	"faustlsp/logging"
	"faustlsp/transport"
	"fmt"
)

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
		// TODO: Validate Message (error if the client shouldn't be sending that message)
		err = s.ValidateMethod(method)
		if err != nil {break}
		
		// TODO: Dispatch to Method Handler

		// Log Current JSON RPC Message

		
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

func (s *Server) ValidateMethod(method string) error{
	switch method {
	case "initialize":
		if s.Status != Created && s.Status != Shutdown {
			return errors.New("Server already initialized")
		}
	default:
		return errors.New("Invalid Method: "+method)
	}
	return nil
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
