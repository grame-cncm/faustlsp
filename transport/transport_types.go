package transport

import "encoding/json"

type URI string
type DocumentURI string

type Message struct {
	Jsonrpc string `json:"jsonrpc"`
}

type RPCMessage struct {
	Jsonrpc string `json:"jsonrpc"`
	Method  string `json:"method"`
}

type RequestMessage struct {
	Message
	ID     interface{}     `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type ResponseMessage struct {
	Message
	ID     interface{}     `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ResponseError  `json:"error,omitempty"`
}

type ResponseError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type NotificationMessage struct {
	Message
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}
