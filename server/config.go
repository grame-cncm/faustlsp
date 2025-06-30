package server

import (
	"encoding/json"
	"faustlsp/logging"
	"faustlsp/transport"
	"faustlsp/util"
	"path/filepath"
)

type FaustProjectConfig struct {
	Command      string      `json:"command,omitempty"`
	Type         string      `json:"type"` // Actually make this enum between Process or Library
	ProcessName  string      `json:"process_name,omitempty"`
	ProcessFiles []util.Path `json:"process_files,omitempty"`
	IncludeDir   []util.Path `json:"include,omitempty"`
	CompilerDiagnostics bool `json:"compiler_diagnostics,omitempty"`
}

// Ideal behavior of config file
// 1) No project config file => Every file has process called process
// 2) Project config file in root => Project compiled from root dir and all files are relative to root dir
// But what if there are multiple projects inside dirs ?
// Answer: 3) Have them have their own project config file
// Project config defined, but no processfile, all files are process

func (w *Workspace) Rel2Abs(relPath string) util.Path {
	return filepath.Join(w.Root, relPath)
}

func (w *Workspace) cleanDiagnostics(s *Server) {
	for _, f := range w.Files {
		if IsFaustFile(f.Path) {
			w.DiagnoseFile(f.Path, s)
		}
	}
}

func (w *Workspace) sendCompilerDiagnostics(s *Server) {
	for _, filePath := range w.config.ProcessFiles {
		path := filepath.Join(w.Root, filePath)
		f, ok := s.Files.Get(path)
		if ok {
			if !f.hasSyntaxErrors {
				uri := util.Path2URI(path)
				diagnosticErrors := getCompilerDiagnostics(filePath, w.Root, w.config)
				d := transport.PublishDiagnosticsParams{
					URI:         transport.DocumentURI(uri),
					Diagnostics: []transport.Diagnostic{diagnosticErrors},
				}
				s.diagChan <- d
			}
		}
	}
}

func (c *FaustProjectConfig) UnmarshalJSON(content []byte) error{
	type Config FaustProjectConfig
	var cfg = Config{
		Command: "faust",
		ProcessName: "process",
		CompilerDiagnostics: true,
	}
	if err := json.Unmarshal(content, &cfg); err != nil {
		return err
	}
	*c = FaustProjectConfig(cfg)
	return nil
}

func (w *Workspace) parseConfig(content []byte) (FaustProjectConfig, error) {
	var config FaustProjectConfig
	err := json.Unmarshal(content, &config)
	if err != nil {
		logging.Logger.Printf("Invalid Project Config file: %s\n", err)
		return FaustProjectConfig{}, err
	}
	// If no process files provided, all .dsp files become process
	if len(config.ProcessFiles) == 0 {
		config.ProcessFiles = w.getFaustDSPRelativePaths()
	}
	return config, nil
}

func (w *Workspace) defaultConfig() FaustProjectConfig {
	logging.Logger.Printf("Using default config file\n")
	var config = FaustProjectConfig{
		Command:      "faust",
		Type:         "process",
		ProcessFiles: w.getFaustDSPRelativePaths(),
		CompilerDiagnostics: true,
	}
	return config
}

func (w *Workspace) getFaustDSPRelativePaths() []util.Path {
	var filePaths = []util.Path{}
	for key, file := range w.Files {
		if IsDSPFile(key) {
			relPath := file.RelPath
			filePaths = append(filePaths, relPath)
		}
	}
	return filePaths
}
