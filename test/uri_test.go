package tests

import (
	"faustlsp/util"
	"fmt"
	"testing"
)

func TestUri2path(t *testing.T) {
	path, _ := util.Uri2path("file:///home/ecm/a.dsp")
	fmt.Println(path)
}

func TestWindowsPath(t *testing.T) {
	paths := []string{"/home/ecm/a.dsp", "C:\\Program\\a"}
	for _, path := range paths {
		fmt.Print(path)
		fmt.Printf(" Is Windows: %t\n", util.IsWindowsPath(path))
	}
}
