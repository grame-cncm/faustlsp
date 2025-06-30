package server

import (
	"faustlsp/logging"
	"faustlsp/transport"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type FaustError struct {
	File    string
	Line    int
	Message string
}

type FaustErrorReportingType uint

const (
	FileError = iota
	Error
	NullError
)

var FaustErrorName = map[FaustErrorReportingType]string{
	FileError: "File Error",
	Error:     "Error",
	NullError: "Unrecognized Error",
}

func (fe FaustErrorReportingType) String() string {
	return FaustErrorName[fe]
}

func getFaustErrorReportingType(s string) FaustErrorReportingType {
	if len(s) < 5 {
		return NullError
	}
	if s[:5] == "ERROR" {
		return Error
	}
	return FileError
}

// TODO: When handling initialize, send diagnostics capability based on whether PATH has faust or some other compiler path provided by project configuration
func getCompilerDiagnostics(path string, dirPath string) transport.Diagnostic {
	cmd := exec.Command("faust", path)
	if dirPath != "" {
		cmd.Dir = dirPath
	}
	var errors strings.Builder
	cmd.Stderr = &errors
	err := cmd.Run()
	faustErrors := errors.String()
	if err == nil {
		return transport.Diagnostic{}
	}

	errorType := getFaustErrorReportingType(faustErrors)

	switch errorType {
	case FileError:
		error := parseFileError(errors.String())
		logging.Logger.Println(error)
		if error.Line > 0 {
			error.Line -= 1
		}
		if error.Line == -1 {
			error.Line = 0
		}
		return transport.Diagnostic{
			Range: transport.Range{
				Start: transport.Position{
					// Lines must be zero-indexed
					Line:      uint32(error.Line),
					Character: 0,
				},
				End: transport.Position{
					Line: uint32(error.Line),
					// TODO: Actually calculate end of line
					Character: 2147483647,
				},
			},
			Message:  error.Message,
			Severity: transport.DiagnosticSeverity(transport.Error),
			Source:   "faust",
		}
	case Error:
		error := parseError(errors.String())
		logging.Logger.Printf("%+v\n", parseError(errors.String()))
		return transport.Diagnostic{
			Range:    transport.Range{},
			Message:  error.Message,
			Severity: transport.DiagnosticSeverity(transport.Error),
			Source:   "faust",
		}
	case NullError:
		logging.Logger.Printf("Unrecognized Error\n")
		return transport.Diagnostic{}
	default:
		return transport.Diagnostic{}
	}
}

func parseFileError(s string) FaustError {
	re := regexp.MustCompile(`(?s)(.+):([-\d]+)\s:\sERROR\s:\s(.*)`)
	captures := re.FindStringSubmatch(s)
	if len(captures) < 4 {
		panic(fmt.Errorf("Expected 4 values in %+v\n", captures))
	}
	line, _ := strconv.Atoi(captures[2])
	return FaustError{File: captures[1], Line: line, Message: captures[3]}
}

func parseError(s string) FaustError {
	re := regexp.MustCompile(`(?s)ERROR\s:\s(.*)`)
	captures := re.FindStringSubmatch(s)
	if len(captures) < 2 {
		panic(fmt.Errorf("Expected 2 values in %+v\n", captures))
	}
	return FaustError{Message: captures[1]}
}
