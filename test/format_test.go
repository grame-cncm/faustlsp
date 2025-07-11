package tests

import (
	"testing"

	"github.com/carn181/faustlsp/server"
)

func TestFormat(t *testing.T) {
	out, err := server.Format([]byte("process=a with {f=2;};"))
	t.Log(string(out), err)
}
