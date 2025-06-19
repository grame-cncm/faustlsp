package tests

import (
	"faustlsp/server"
	"faustlsp/transport"
	"fmt"
	"testing"
	"unicode/utf8"
)

// TODO: Write actual tests here

func TestGetLines(t *testing.T){
	testStr:= "abcde"
	indices := server.GetLineIndices(testStr)
	fmt.Println(testStr)
	fmt.Println(indices)
	fmt.Println(len(testStr))
	for _, c  := range indices {
		if c >= uint(len(indices)) {
			break
		}
		if c != 0 && testStr[c] != '\n'{
			t.Errorf("%c at %d not newline", testStr[c], c)
		}
	}
	//	t.Error("")
}

func TestPositionToOffset(t *testing.T){
	testStr:= "abcd\nef😆abc\nasd😆\n45"
	offset, _ := server.PositionToOffset(transport.Position{0, 4}, testStr)
	offset=offset
	v, _ := utf8.DecodeRuneInString(testStr[offset:])
	fmt.Printf("At %d is [%d]%c\n", offset, v, v)
	//	t.Error("")
}
