package transport 

import (
	"bytes"
	"bufio"
	"errors"
	"faustlsp/logging"
	"io"
	"net"
	"os"
	"strconv"
)

type TransportMethod int
const (
	Stdin   = iota
	Socket
)

// Transport structure to handle reading from streams
type Transport struct {
	Method TransportMethod     // type of stream
	Scanner *bufio.Scanner     // stream scanner 
	Closed bool
}

func (t *Transport) Init(method TransportMethod){
	t.Method = method
	var r io.Reader
	switch t.Method {
	// Communicate with client through stdin
	case Stdin:
		r = os.Stdin
	// Communicate with client through tcp socket
	// Default port at 5007
	// TODO: take port from cmd arguments
	case Socket:
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
	scanner.Split(split)
	t.Scanner = scanner
}

// Reads one JSON RPC message from the stream
func (t *Transport) Read() ([]byte, error) {
	t.Closed = !t.Scanner.Scan()
	return t.Scanner.Bytes(), t.Scanner.Err()
}

// Split function for scanner to parse a JSON RPC message
func split(data []byte, _ bool) (advance int, token []byte, err error) {
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

