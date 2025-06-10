package transport_test

import (
	"bytes"
	"faustlsp/transport"
	"testing"
)

func TestSocket(test *testing.T) {
	expectedMsg := []byte("Content-Length: 4\r\n\r\nHey!")	
	client := func(){
		var t transport.Transport
		t.Init(transport.Client, transport.Socket)

		err := t.Write([]byte("Hey!"))
		if err != nil {
			test.Fatal(err)
		}
		
		t.Close()
	}

	server := func(){
		var t transport.Transport

		t.Init(transport.Server, transport.Socket)

		msg, err := t.Read()
		if err != nil {
			test.Fatal(err)
		}

		bytes.Equal(msg, expectedMsg)
		if !bytes.Equal(msg, expectedMsg) {
			test.Fatalf("Got different message: %s\n", string(msg))
		}
		
		t.Close()
	}

	go server()
	client()
}
