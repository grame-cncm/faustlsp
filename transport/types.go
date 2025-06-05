package transport

import "encoding/json"

type Message struct {
	Jsonrpc string `json:"jsonrpc"`
}

type RPCMessage struct {
	Jsonrpc string `json:"jsonrpc"`
	Method string      `json:"method"`
}

type RequestMessage struct {
	Message
	ID     interface{} `json:"id"`
	Method string      `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type ResponseMessage struct {
	Message
	ID     interface{} `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ResponseError `json:"error,omitempty"`
}

type ResponseError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type NotificationMessage struct {
	Message
	Method string      `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

const (
	ParseError                    int = -32700
	InvalidRequest                int = -32600
	MethodNotFound                int = -32601
	InvalidParams                 int = -32602
	InternalError                 int = -32603
	JSONRPCReservedErrorRangeStart int = -32099
	ServerErrorStart              int = JSONRPCReservedErrorRangeStart
	ServerNotInitialized          int = -32002
	UnknownErrorCode              int = -32001
	JSONRPCReservedErrorRangeEnd   int = -32000
	ServerErrorEnd                int = JSONRPCReservedErrorRangeEnd
	LSPReservedErrorRangeStart    int = -32899
	RequestFailed                 int = -32803
	ServerCancelled               int = -32802
	ContentModified               int = -32801
	RequestCancelled              int = -32800
	LSPReservedErrorRangeEnd      int = -32800
)
