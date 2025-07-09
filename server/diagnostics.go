package server

import (
	"encoding/json"
	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/transport"
)

func (s *Server) GenerateDiagnostics() {
	s.diagChan = make(chan transport.PublishDiagnosticsParams)

	for {
		logging.Logger.Printf("Waiting for diagnostic\n")
		select {
		case diag := <-s.diagChan:
			content, _ := json.Marshal(diag)
			logging.Logger.Printf("Writing Diagnostic Message: %s\n", content)
			s.Transport.WriteNotif("textDocument/publishDiagnostics", content)
		}
	}
}
