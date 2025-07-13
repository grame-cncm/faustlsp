package util

import (
	"net/url"
	"path/filepath"
	"runtime"

	"strings"
	"unicode"

	"github.com/carn181/faustlsp/logging"
)

type Path = string
type Uri = string

func Uri2path(uri string) (string, error) {
	logging.Logger.Info("Trying to parse URI", "uri", uri)
	url, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	//	url.Path
	logging.Logger.Info("Parsed url as path", "path", url)
	if IsWindowsDriveURIPath(url.Path) {
		url.Path = strings.ToUpper(string(url.Path[1])) + url.Path[2:]
	}
	return filepath.FromSlash(url.Path), nil
}

func Path2URI(path string) Uri {
	scheme := "file://"
	if runtime.GOOS == "windows" {
		path = "/" + strings.Replace(path, "\\", "/", -1)
	}
	return scheme + path
}

func IsWindowsDriveURIPath(uri string) bool {
	if len(uri) < 4 {
		return false
	}
	return uri[0] == '/' && unicode.IsLetter(rune(uri[1])) && uri[2] == ':'
}

func IsWindowsDrivePath(path string) bool {
	if len(path) < 3 {
		return false
	}
	return unicode.IsLetter(rune(path[0])) && path[1] == ':'
}
