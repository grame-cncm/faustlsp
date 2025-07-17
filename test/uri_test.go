package tests

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/carn181/faustlsp/util"
)

func TestUri2path(t *testing.T) {

	if runtime.GOOS != "windows" {
		path, _ := util.URI2path("file:///home/user/a.dsp")
		if !(path == "/home/user/a.dsp") {
			t.Fatalf("Invalid Unix Path %s\n", path)
		}
	} else {
		path, _ := util.URI2path("file:///C:/users/user/file.txt")
		if !(path == "C:\\users\\user\\file.txt") {
			t.Fatalf("Invalid Windows Path %s\n", path)
		}
	}
}

func TestPath2Uri(t *testing.T) {
	if runtime.GOOS == "windows" {
		path := "C:\\user\\a.dsp"
		uri := util.Path2URI(path)
		expected_uri := "file:///C:/user/a.dsp"
		if !(uri == expected_uri) {
			t.Fatalf("Invalid URI %s\n", uri)
		}
	} else {
		path := "/home/user/a.dsp"
		uri := util.Path2URI(path)
		expected_uri := "file:///home/user/a.dsp"
		if !(uri == expected_uri) {
			t.Fatalf("Invalid URI %s\n", uri)
		}
	}
}

func TestIsWindowsPath(t *testing.T) {
	paths := []string{"/home/user/a.dsp", "C:\\Program\\a"}
	for _, path := range paths {
		fmt.Print(path)
		fmt.Printf(" Is Windows: %t\n", util.IsWindowsDrivePath(path))
	}
}
