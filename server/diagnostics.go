package server

import (
	"encoding/json"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/transport"
)

func (s *Server) GenerateDiagnostics() {
	s.diagChan = make(chan transport.PublishDiagnosticsParams)

	for {
		logging.Logger.Info("Waiting for diagnostic\n")
		select {
		case diag := <-s.diagChan:
			content, _ := json.Marshal(diag)
			logging.Logger.Info("Writing Diagnostic", "content", string(content))
			s.Transport.WriteNotif("textDocument/publishDiagnostics", content)
		}
	}
}
