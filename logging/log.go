package logging

import (
	"log"
	"os"
	"runtime"
)

// logPath defines the default log file path based on OS.
var logPath string

// Logger is the global logger instance.
var Logger *log.Logger

// Init initializes the logger with a file output.
func Init() {
	// TODO: Add proper default log file path for windows
	// TODO: Add option to take log file path from user
	// Determine the log file path based on the operating system.
	switch runtime.GOOS {
	case "windows":
		logPath = "faust-lsp-log.txt"
	case "linux", "darwin", "freebsd", "openbsd", "netbsd", "plan9":
		logPath = "/tmp/faust-lsp-log.txt"
	default:
		logPath = "faust-lsp-log.txt"
	}

	// Open the log file.  Create it if it doesn't exist.
	f, err := os.OpenFile(logPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		panic(err)
	}
	// Initialize the logger to write to the file, without flags or prefixes.
	Logger = log.New(f, "faust-lsp: ", log.Ltime)
}
