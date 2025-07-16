package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/transport"
	"github.com/carn181/faustlsp/util"
)

func Format(content []byte, indent string) ([]byte, error) {
	// TODO: Allow to take faustExec and customQueryFile from config file
	faustExec := "faustfmt"

	// Check if formatter exists in path
	_, err := exec.LookPath(faustExec)
	if err != nil {
		return []byte{}, errors.New("Couldn't find " + faustExec + " in PATH")
	}

	// Setup faustfmt command with input
	var errs strings.Builder
	var output bytes.Buffer
	cmd := exec.Command(faustExec, "-i", indent)
	cmd.Stdin = bytes.NewBuffer(content)
	cmd.Stderr = &errs
	cmd.Stdout = &output

	// Run faustfmt command
	err = cmd.Run()
	if err != nil {
		return []byte{}, fmt.Errorf("faustfmt error: %s, Stderr: %s", err, errs.String())
	}

	return output.Bytes(), nil
}

func GetIndent(par transport.DocumentFormattingParams) string {
	if par.Options.InsertSpaces {
		s := ""
		for range par.Options.TabSize {
			s += " "
		}
		return s
	} else {
		return "\t"
	}
}

func Formatting(ctx context.Context, s *Server, par json.RawMessage) (json.RawMessage, error) {
	var params transport.DocumentFormattingParams
	json.Unmarshal(par, &params)

	logging.Logger.Info("Formatting request", "params", string(par))
	path, err := util.Uri2path(string(params.TextDocument.URI))
	if err != nil {
		logging.Logger.Error("Uri2path error", "error", err)
	}

	f, ok := s.Files.Get(path)
	content := f.Content
	var output []byte
	if ok {
		output, err = Format(content, GetIndent(params))
		if err != nil {
			logging.Logger.Error("Format error", "error", err)
		}
	}
	logging.Logger.Info("Got this for formatting", "output", string(output))

	endPos := transport.Position{Line: 0, Character: 0}
	if ok {
		endPos, err = getDocumentEndPosition(string(content), string(s.Files.encoding))
		if err != nil {
			logging.Logger.Error("OffsetToPosition error", "error", err)
			endPos = transport.Position{Line: 0, Character: 0}
		}
	}

	edit := transport.TextEdit{
		Range: transport.Range{
			Start: transport.Position{Line: 0, Character: 0},
			End:   endPos,
		},
		NewText: string(output),
	}
	resultBytes, err := json.Marshal([]transport.TextEdit{edit})

	return resultBytes, err
}
