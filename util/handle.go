package util

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
)

type Path = string
type URI = string

type Handle struct {
	URI  URI
	Path Path
}

func FromPath(path string) Handle {
	return Handle{Path2URI(path), path}
}

func FromURI(uri string) (Handle, error) {
	path, err := URI2path(uri)
	return Handle{uri, path}, err
}

// Converting functions

func URI2path(uri string) (string, error) {
	url, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	//	url.Path
	if IsWindowsDriveURIPath(url.Path) {
		url.Path = strings.ToUpper(string(url.Path[1])) + url.Path[2:]
	}
	return filepath.FromSlash(url.Path), nil
}

func Path2URI(path string) URI {
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
