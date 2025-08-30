package server

import (
	"context"
	"encoding/json"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/transport"
	"github.com/carn181/faustlsp/util"
)

func Completion(ctx context.Context, s *Server, par json.RawMessage) (json.RawMessage, error) {
	logging.Logger.Info("Got Completion Request", "request", string(par))

	var params transport.CompletionParams
	json.Unmarshal(par, &params)

	handle, err := util.FromURI(string(params.TextDocument.URI))
	if err != nil {
		return []byte("null"), err
	}
	results := GetPossibleSymbols(params.Position, handle.Path, &s.Store, string(s.Files.encoding))

	var items = []transport.CompletionItem{}
	for _, sym := range results {
		items = append(items, transport.CompletionItem{
			Label: sym.name,
			Kind:  transport.VariableCompletion,
			// Documentation: &transport.Or_CompletionItem_documentation{
			//	Value: transport.MarkupContent{
			//		Kind:  "plaintext",
			//		Value: sym.docs.Full,
			//	},
			// },
			// Detail: sym.docs.Usage,
		})
	}

	logging.Logger.Info("Completion results", "results", items)

	resp, err := json.Marshal(items)
	if err != nil {
		return []byte("null"), err
	}
	return resp, nil
}
