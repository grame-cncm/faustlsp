package tests

import (
	"fmt"
	"testing"
	"unicode/utf8"

	"github.com/carn181/faustlsp/server"
	"github.com/carn181/faustlsp/transport"
)

// TODO: Write actual tests here

func TestGetLines(t *testing.T) {
	testStr := "abcde"
	indices := server.GetLineIndices(testStr)
	fmt.Println(testStr)
	fmt.Println(indices)
	fmt.Println(len(testStr))
	for _, c := range indices {
		if c >= uint(len(indices)) {
			break
		}
		if c != 0 && testStr[c] != '\n' {
			t.Errorf("%c at %d not newline", testStr[c], c)
		}
	}
	//	t.Error("")
}


func TestPositionToOffset(t *testing.T) {
	testStr := "abcd\nef😆abc\nasd😆\n45"
	offset, _ := server.PositionToOffset(transport.Position{Line: 0, Character: 4}, testStr, "utf-16")
	v, _ := utf8.DecodeRuneInString(testStr[offset:])
	fmt.Printf("At %d is [%d]%c\n", offset, v, v)
	//	t.Error("")

}

type IncrementalTest struct {
	text string
	pos transport.Position
	off uint
}

func testIncremental(t IncrementalTest) error {
	p, _ := server.OffsetToPosition(t.off, t.text, "utf-16")
	o, _ := server.PositionToOffset(t.pos, t.text, "utf-16")
	if p != t.pos {
		return fmt.Errorf("Expected: %v, Found: %v\n", t.pos, p)
	}
	if o != t.off {
		return fmt.Errorf("Expected: %v, Found: %v\n", t.off, o)
	}
	return nil
}

func TestOffset2Position(t *testing.T){
	testStr := `import("stdfaust.lib");
process = os.osc(400);


h = a
with {

};;
`
	//	testStr2 := "import(\"stdfaust.lib\");\nprocess = os.osc(400);\n\nh = a\n    with {\n    };\n\n"
	itests := []IncrementalTest{
		{testStr, transport.Position{0,0}, 0,},	
		{testStr, transport.Position{8,0}, uint(len(testStr)),},
	}
	for _, test := range itests {
		t.Log(fmt.Sprint(testIncremental(test)))
	}
}
