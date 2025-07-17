package tests

import (
	"encoding/json"
	"testing"

	"github.com/carn181/faustlsp/transport"
)

func TestResponseType(t *testing.T) {
	r1 := transport.ResponseMessage{
		Message: transport.Message{Jsonrpc: "2.0"},
		ID:      1,
		Result:  []byte(""),
		Error:   nil,
	}
	msg, _ := json.Marshal(r1)
	t.Log(string(msg))
}
