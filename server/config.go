package server

import (
	"encoding/json"
	"path/filepath"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/transport"
	"github.com/carn181/faustlsp/util"
)

type FaustProjectConfig struct {
	Command             string      `json:"command,omitempty"`
	Type                string      `json:"type"` // Actually make this enum between Process or Library eventually
	ProcessName         string      `json:"process_name,omitempty"`
	ProcessFiles        []util.Path `json:"process_files,omitempty"`
	IncludeDir          []util.Path `json:"include,omitempty"`
	CompilerDiagnostics bool        `json:"compiler_diagnostics,omitempty"`
}

func (w *Workspace) Rel2Abs(relPath string) util.Path {
	return filepath.Join(w.Root, relPath)
}

func (w *Workspace) cleanDiagnostics(s *Server) {
	for _, path := range w.Files {
		f, _ := s.Files.GetFromPath(path)
		f.mu.RLock()
		path := f.Handle.Path
		f.mu.RUnlock()
		if IsFaustFile(path) {
			w.DiagnoseFile(path, s)
		}
	}
}

func (w *Workspace) sendCompilerDiagnostics(s *Server) {
	for _, filePath := range w.Config.ProcessFiles {
		path := filepath.Join(w.Root, filePath)
		f, ok := s.Files.GetFromPath(path)

		if ok {
			f.mu.RLock()
			tempPath := w.TempDirPath(f.Handle.Path)
			logging.Logger.Info("Generating Compiler Diagnostics", "temp_path", tempPath)
			f.mu.RUnlock()
			if !f.hasSyntaxErrors {
				var diagnosticErrors = []transport.Diagnostic{}
				uri := util.Path2URI(path)
				logging.Logger.Info("Generating Compiler Diagnostics", "temp_path", tempPath)
				diagnosticError := getCompilerDiagnostics(tempPath, w.Root, w.Config)
				if diagnosticError.Message != "" {
					diagnosticErrors = []transport.Diagnostic{diagnosticError}
				}
				d := transport.PublishDiagnosticsParams{
					URI:         transport.DocumentURI(uri),
					Diagnostics: diagnosticErrors,
				}
				s.diagChan <- d
			}
		}
	}
}

func (c *FaustProjectConfig) UnmarshalJSON(content []byte) error {
	type Config FaustProjectConfig
	var cfg = Config{
		Command:             "faust",
		ProcessName:         "process",
		CompilerDiagnostics: true,
	}
	if err := json.Unmarshal(content, &cfg); err != nil {
		logging.Logger.Error("Failed to unmarshal FaustProjectConfig", "error", err)
		return err
	}
	*c = FaustProjectConfig(cfg)
	return nil
}

func (w *Workspace) parseConfig(content []byte) (FaustProjectConfig, error) {
	var config FaustProjectConfig
	err := json.Unmarshal(content, &config)
	if err != nil {
		logging.Logger.Error("Invalid Project Config file", "error", err)
		return FaustProjectConfig{}, err
	}
	// If no process files provided, all .dsp files become process
	if len(config.ProcessFiles) == 0 {
		config.ProcessFiles = w.getFaustDSPRelativePaths()
	}
	return config, nil
}

func (w *Workspace) defaultConfig() FaustProjectConfig {
	logging.Logger.Info("Using default config file")
	var config = FaustProjectConfig{
		Command:             "faust",
		Type:                "process",
		ProcessFiles:        w.getFaustDSPRelativePaths(),
		CompilerDiagnostics: true,
	}
	return config
}

func (w *Workspace) getFaustDSPRelativePaths() []util.Path {
	var filePaths = []util.Path{}
	for _, file := range w.Files {
		if IsDSPFile(file) {
			filePaths = append(filePaths, file)
		}
	}
	return filePaths
}
