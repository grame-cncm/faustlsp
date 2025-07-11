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
		return []byte{}, errors.New(fmt.Sprintf("%s, Stderr: %s\n", err, errs.String()))
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

func Formatting(ctx context.Context, s *Server, id any, par json.RawMessage) (json.RawMessage, error) {
	var params transport.DocumentFormattingParams
	json.Unmarshal(par, &params)

	logging.Logger.Printf("Formatting for %s\n", string(par))
	path, err := util.Uri2path(string(params.TextDocument.URI))
	if err != nil {
		logging.Logger.Print(err)
	}

	f, ok := s.Files.Get(path)
	var output []byte
	if ok {
		output, err = Format(f.Content, GetIndent(params))
		if err != nil {
			logging.Logger.Print(err)
		}
	}
	logging.Logger.Printf("Got this for formatting: '%s'", string(output))

	end, err := OffsetToPosition(uint(len(output)),
		string(output),
		string(s.Files.encoding))
	if err != nil {
		logging.Logger.Println(err)
	}
	edit := transport.TextEdit{
		Range: transport.Range{
			Start: transport.Position{Line: 0, Character: 0},
			End:   end,
		},
		NewText: string(output),
	}
	resultBytes, _ := json.Marshal([]transport.TextEdit{edit})

	var resp transport.ResponseMessage = transport.ResponseMessage{
		Message: transport.Message{Jsonrpc: "2.0"},
		ID:      id,
		Result:  resultBytes,
	}

	msg, err := json.Marshal(resp)
	return msg, err
}
