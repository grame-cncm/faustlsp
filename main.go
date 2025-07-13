package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/server"
	"github.com/carn181/faustlsp/transport"
)

func main() {
	logging.Init()

	logging.Logger.Info("Initialized")

	// Background Context for cancelling
	ctx, cancel := context.WithCancel(context.Background())

	var s server.Server

	// Default Transport method is stdin
	s.Init(transport.Stdin)

	// Handle Signals
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		// Cancel when a signal is received
		<-sigs
		cancel()
		fmt.Println("Got Interrupt ")
		logging.Logger.Info("Got Interrupt")
	}()

	// Start running server
	err := s.Run(ctx)
	logging.Logger.Info("Ended")

	if err != nil {
		os.Exit(1)
	} else {
		os.Exit(0)
	}
}
