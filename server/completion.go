package server

import (
	"context"
	"encoding/json"
	"unicode"

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

	replaceRange := transport.Range{}
	f, ok := s.Files.Get(handle)
	if ok {
		f.mu.RLock()
		replaceRange = FindCompletionReplaceRange(params.Position, string(f.Content), string(s.Files.encoding))
		logging.Logger.Info("Replace Range", "range", replaceRange)
		f.mu.RUnlock()
	}
	var items = []transport.CompletionItem{}
	plainText := transport.PlainTextTextFormat
	for _, sym := range results {
		items = append(items, transport.CompletionItem{
			Label: sym.name,
			Kind:  transport.VariableCompletion,
			//			InsertText: sym.name,
			InsertTextFormat: &plainText,
			TextEdit: transport.TextEdit{
				NewText: sym.name,
				Range:   replaceRange,
			},

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

func FindCompletionReplaceRange(pos transport.Position, content, encoding string) transport.Range {

	offset, err := PositionToOffset(pos, content, encoding)
	if err != nil {
		return transport.Range{}
	}
	start, end := offset, offset
	for {
		logging.Logger.Info("Finding start", "start", start, "char", string(content[start]))
		if start <= 0 {
			break
		}
		if !unicode.IsLetter(rune(content[start-1])) && !unicode.IsDigit(rune(content[start-1])) {
			break
		}
		start--
	}
	startPos, err := OffsetToPosition(start, content, encoding)
	endPos, err := OffsetToPosition(end, content, encoding)
	return transport.Range{
		Start: startPos,
		End:   endPos,
	}
}
