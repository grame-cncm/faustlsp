package tests

import (
	"context"
	"encoding/json"
	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/server"
	"github.com/carn181/faustlsp/transport"

	"testing"
	"time"
)

func TestExitWithoutError(t *testing.T) {
	logging.Init()
	logging.Logger.Println("Starting")
	var s server.Server

	runserver := func() error {
		s.Init(transport.Socket)
		err := s.Run(context.Background())
		s.Transport.Close()
		return err
	}

	ctr := 0

	go func() {
		var tr transport.Transport
		tr.Init(transport.Client, transport.Socket)
		msg, _ := json.Marshal(transport.ParamInitialize{
			XInitializeParams: transport.XInitializeParams{
				RootPath: "",
			},
		})
		tr.WriteRequest(ctr, "initialize", msg)
		tr.Read()
		tr.WriteRequest(ctr+1, "shutdown", msg)
		tr.WriteNotif("exit", msg)
		time.Sleep(1 * time.Second)
		tr.Close()

	}()
	err := runserver()
	if err != nil {
		t.Errorf("Exit was not graceful, when it should've been")
	}
}

func TestExitWithError(t *testing.T) {
	logging.Init()
	logging.Logger.Println("Starting")

	var s server.Server
	ctx, cancel := context.WithCancel(context.Background())
	runserver := func() error {
		s.Init(transport.Socket)
		err := s.Run(ctx)
		return err
	}

	ctr := 0

	go func() {
		var tr transport.Transport
		tr.Init(transport.Client, transport.Socket)
		msg, _ := json.Marshal(transport.ParamInitialize{
			XInitializeParams: transport.XInitializeParams{
				RootPath: "",
			},
		})
		tr.WriteRequest(ctr, "initialize", msg)
		tr.Read()
		tr.WriteNotif("exit", msg)
		time.Sleep(1 * time.Second)
		tr.Close()
		cancel()

	}()
	err := runserver()
	if err.Error() != "Exiting Ungracefully" {
		t.Errorf("Exit should not have been graceful")
	}
}
