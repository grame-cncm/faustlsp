package main

import (
	"faustlsp/logging"
	"faustlsp/server"
	"faustlsp/transport"
	"fmt"
)

func main(){
	logging.Init()
	logging.Logger.Println("Initialized")
	
	var s server.Server
	s.Init(transport.Stdin)
	err := s.Loop()

	if err != nil {
		fmt.Println(err)
		logging.Logger.Println(err)
	}
	logging.Logger.Println("Ended")
	return;
}
