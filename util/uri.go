package util

import (
	"net/url"
	"unicode"
)

type Path = string
type Uri = string

func Uri2path(uri string) (string, error) {
	url, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	//	url.Path
	return url.Path, nil
}

func IsWindowsPath(path string) bool {
	if len(path) < 3 {
		return false
	}
	return unicode.IsLetter(rune(path[0])) && path[1] == ':'

}
