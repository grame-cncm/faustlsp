package main

import (
	"context"
	"faustlsp/logging"
	"faustlsp/server"
	"faustlsp/transport"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main(){
	logging.Init()
	logging.Logger.Println("Initialized")

	// Background Context for cancelling
	ctx, cancel := context.WithCancel(context.Background())
	
	var s server.Server

	// Default Transport method is stdin
	s.Init(transport.Stdin)

	// Handle Signals
	sigs := make(chan os.Signal, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func(){
		// Cancel when a signal is received
		<-sigs
		cancel()
		fmt.Println("Got Interrupt ")
		logging.Logger.Println("Got Interrupt")
	}()

	// Start running server
	err := s.Run(ctx)
	logging.Logger.Println("Ended")	
	
	if err != nil {
		os.Exit(1)
	} else {
		os.Exit(0)
	}
}
