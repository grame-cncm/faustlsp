package server

import (
	"faustlsp/logging"
	"faustlsp/transport"
)

type ServerState int 

const (
	Created       = iota
	Initializing
	Initialized
	Shutdown
)

// Main Server Struct
type Server struct {
	Status ServerState
	// Allows to add other transportation methods in the future
	// possible values: stdin | socket
	Transport transport.Transport	
}

func (s *Server) Init(transport transport.TransportMethod){
	s.Status = Created
	s.Transport.Init(transport)
	return;
}

// The central LSP server loop
func (s *Server) Loop() error{
	var err error
	var msg []byte
	// LSP Server Main Loop
	for s.Status != Shutdown && !s.Transport.Closed && err == nil{
		msg, err = s.Transport.Read()

		logging.Logger.Println("Got "+string(msg))
	}

	return err
}
