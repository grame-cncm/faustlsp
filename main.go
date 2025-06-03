package main

import (
	"bufio"
	"bytes"
	"errors"
	"faustlsp/logging"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
)

func main(){
	logging.Init()
	logging.Logger.Println("Initialized")
	
	// Allows to add other transportation methods in the future
	// possible values: stdin | socket
	var transportMethod string

	// By default, communicate through stdin
	transportMethod = "stdin"
	
	var r io.Reader
	switch transportMethod {
	// Communicate with client through stdin
	case "stdin":
		r = os.Stdin
	// Communicate with client through tcp socket
	// Default port at 5007
	// TODO: take port from cmd arguments
	case "socket":
		ln, err := net.Listen("tcp",":5007")
		if err != nil {
			logging.Logger.Fatal(err)
		}
		conn, err := ln.Accept()
		if err != nil {
			logging.Logger.Fatal(err)
		}
		r = conn
	}
	
	scanner := bufio.NewScanner(r)
	scanner.Split(Split)

	for scanner.Scan() {
		msg := scanner.Bytes()

		logging.Logger.Println("Got "+string(msg))
	}

	err := scanner.Err()
	if err != nil {
		fmt.Println(err)
		logging.Logger.Println(err)
	}
	logging.Logger.Println("Ended")
	return;
}

func Split(data []byte, _ bool) (advance int, token []byte, err error) {
	header, content, found := bytes.Cut(data, []byte{'\r', '\n', '\r', '\n'})
	if !found {
		return 0, nil, nil
	}

	// Content-Length: <number>
	if len(header) < len("Content-Length: ") {
		return 0, nil, errors.New("Invalid Header: "+string(header))
	}
	contentLengthBytes := header[len("Content-Length: "):]
	contentLength, err := strconv.Atoi(string(contentLengthBytes))
	if err != nil {
		return 0, nil, errors.New("Invalid Content Length")
	}

	if len(content) < contentLength {
		return 0, nil, nil
	}

	totalLength := len(header) + 4 + contentLength
	return totalLength, data[:totalLength], nil
}
