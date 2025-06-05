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

// Useful for socket dialling or listening based on client and server
type TransportType int
const (
	Client = iota
	Server
)

// Transport structure to handle reading from streams
type Transport struct {
	Type   TransportType       // client or server
	Method TransportMethod     // type of stream
	Scanner *bufio.Scanner     // reader (scanner)
	conn   net.Conn            // connection if socket
	Writer io.Writer            // writer
	Closed bool
}

func (t *Transport) Init(ttype TransportType, method TransportMethod){
	t.Method = method
	t.Type = ttype
	var r io.Reader
	
	switch t.Method {
	// Communicate with client through stdin
	case Stdin:
		r = os.Stdin
		t.Writer = os.Stdout

	// Communicate with client through tcp socket
	// Default port at 5007
	// TODO: take port from cmd arguments
	case Socket:
		var conn net.Conn

		switch t.Type {
		case Server:
			ln, err := net.Listen("tcp",":5007")
			if err != nil {
				logging.Logger.Fatal(err)
			}
			conn, err = ln.Accept()
			if err != nil {
				logging.Logger.Fatal(err)
			}
		case Client:
			var err error
			conn, err = net.Dial("tcp", "localhost:5007")
			t.conn = conn
			if err != nil {
				logging.Logger.Fatal(err)
			}
		}
		r = conn
		t.Writer = conn
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

// Writes JSON RPC message
// TODO: Validate message as JSON RPC before sending ?
func (t *Transport) Write(msg []byte) error {
	_, err := t.Writer.Write(msg)
	return err
}

func (t *Transport) Close() {
	if t.Type == Client {
		t.conn.Close()
	}
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

