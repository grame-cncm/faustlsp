package tests

import (
	"fmt"
	"testing"

	"github.com/carn181/faustlsp/server"
	"github.com/carn181/faustlsp/transport"
)

func TestGetLines(t *testing.T) {
	testStr := "abcde"
	indices := server.GetLineIndices(testStr)
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
	tests := []struct {
		name     string
		text     string
		pos      transport.Position
		encoding string
		want     uint
		wantErr  bool
	}{
		{
			name:     "Empty string, position 0,0",
			text:     "",
			pos:      transport.Position{Line: 0, Character: 0},
			encoding: "utf-16",
			want:     0,
			wantErr:  false,
		},
		{
			name:     "Single line, position at end",
			text:     "abc",
			pos:      transport.Position{Line: 0, Character: 3},
			encoding: "utf-16",
			want:     3,
			wantErr:  false,
		},
		{
			name:     "Single line, position out of bounds",
			text:     "abc",
			pos:      transport.Position{Line: 0, Character: 10},
			encoding: "utf-16",
			want:     3,
			wantErr:  false,
		},
		{
			name:     "Multi-line, start of second line",
			text:     "abc\ndef",
			pos:      transport.Position{Line: 1, Character: 0},
			encoding: "utf-16",
			want:     4,
			wantErr:  false,
		},
		{
			name:     "Multi-line, end of second line",
			text:     "abc\ndef",
			pos:      transport.Position{Line: 1, Character: 3},
			encoding: "utf-16",
			want:     7,
			wantErr:  false,
		},
		{
			name:     "Position beyond last line",
			text:     "abc\ndef",
			pos:      transport.Position{Line: 2, Character: 0},
			encoding: "utf-16",
			want:     7,
			wantErr:  false,
		},
		{
			name:     "Unicode character, utf-16 encoding",
			text:     "a😆b\nc",
			pos:      transport.Position{Line: 0, Character: 3},
			encoding: "utf-16",
			want:     5, // 'a' + '😆' (2 code units in utf-16, but offset is still 2 in Go string)
			wantErr:  false,
		},
		{
			name:     "Unicode character, utf-32 encoding",
			text:     "a😆b\nc",
			pos:      transport.Position{Line: 0, Character: 2},
			encoding: "utf-32",
			want:     5,
			wantErr:  false,
		},
		{
			name:     "Position at start of file",
			text:     "abc\ndef",
			pos:      transport.Position{Line: 0, Character: 0},
			encoding: "utf-16",
			want:     0,
			wantErr:  false,
		},
		{
			name:     "Position at end of first line",
			text:     "abc\ndef",
			pos:      transport.Position{Line: 0, Character: 3},
			encoding: "utf-16",
			want:     3,
			wantErr:  false,
		},
		{
			name:     "Position at start of second line",
			text:     "abc\ndef",
			pos:      transport.Position{Line: 1, Character: 0},
			encoding: "utf-16",
			want:     4,
			wantErr:  false,
		},
		{
			name:     "Position at end of second line",
			text:     "abc\ndef",
			pos:      transport.Position{Line: 1, Character: 3},
			encoding: "utf-16",
			want:     7,
			wantErr:  false,
		},
		{
			name:     "Position beyond last line",
			text:     "abc\ndef",
			pos:      transport.Position{Line: 2, Character: 0},
			encoding: "utf-16",
			want:     7,
			wantErr:  false,
		},
		{
			name:     "Position beyond last character in line",
			text:     "abc\ndef",
			pos:      transport.Position{Line: 1, Character: 100},
			encoding: "utf-16",
			want:     7,
			wantErr:  false,
		},
		{
			name:     "Empty string, position 0,0",
			text:     "",
			pos:      transport.Position{Line: 0, Character: 0},
			encoding: "utf-16",
			want:     0,
			wantErr:  false,
		},
		{
			name:     "String with only newlines",
			text:     "\n\n\n",
			pos:      transport.Position{Line: 2, Character: 0},
			encoding: "utf-16",
			want:     2,
			wantErr:  false,
		},
		{
			name:     "Unicode emoji, position at emoji",
			text:     "a💚c",
			pos:      transport.Position{Line: 0, Character: 2},
			encoding: "utf-16",
			want:     5,
			wantErr:  false,
		},
		{
			name:     "Unicode emoji, position after emoji",
			text:     "a💚c",
			pos:      transport.Position{Line: 0, Character: 3},
			encoding: "utf-16",
			want:     5,
			wantErr:  false,
		},
		{
			name:     "Tabs and spaces",
			text:     "a\tb c\n d",
			pos:      transport.Position{Line: 1, Character: 2},
			encoding: "utf-16",
			want:     8,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := server.PositionToOffset(tt.pos, tt.text, tt.encoding)
			if (err != nil) != tt.wantErr {
				t.Errorf("PositionToOffset() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("PositionToOffset() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOffsetToPosition(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		offset   uint
		encoding string
		want     transport.Position
		wantErr  bool
	}{
		{
			name:     "Empty string, offset 0",
			text:     "",
			offset:   0,
			encoding: "utf-16",
			want:     transport.Position{Line: 0, Character: 0},
			wantErr:  false,
		},
		{
			name:     "Single line, offset at end",
			text:     "abc",
			offset:   3,
			encoding: "utf-16",
			want:     transport.Position{Line: 0, Character: 3},
			wantErr:  false,
		},
		{
			name:     "Single line, offset out of bounds",
			text:     "abc",
			offset:   10,
			encoding: "utf-16",
			want:     transport.Position{Line: 0, Character: 3},
			wantErr:  false,
		},
		{
			name:     "Multi-line, start of second line",
			text:     "abc\ndef",
			offset:   4,
			encoding: "utf-16",
			want:     transport.Position{Line: 1, Character: 0},
			wantErr:  false,
		},
		{
			name:     "Multi-line, end of second line",
			text:     "abc\ndef",
			offset:   7,
			encoding: "utf-16",
			want:     transport.Position{Line: 1, Character: 3},
			wantErr:  false,
		},
		{
			name:     "Offset beyond last character",
			text:     "abc\ndef",
			offset:   100,
			encoding: "utf-16",
			want:     transport.Position{Line: 1, Character: 3},
			wantErr:  false,
		},
		{
			name:     "Unicode character, utf-16 encoding",
			text:     "a😆b\nc",
			offset:   5,
			encoding: "utf-16",
			want:     transport.Position{Line: 0, Character: 3},
			wantErr:  false,
		},
		{
			name:     "Unicode emoji, position at emoji",
			text:     "a💚c",
			offset:   5,
			encoding: "utf-16",
			want:     transport.Position{Line: 0, Character: 3},
			wantErr:  false,
		},
		{
			name:     "Tabs and spaces",
			text:     "a\tb c\n d",
			offset:   8,
			encoding: "utf-16",
			want:     transport.Position{Line: 1, Character: 2},
			wantErr:  false,
		},
		{
			name:     "String with only newlines",
			text:     "\n\n\n",
			offset:   2,
			encoding: "utf-16",
			want:     transport.Position{Line: 2, Character: 0},
			wantErr:  false,
		},
		{
			name:     "Offset at start of file",
			text:     "abc\ndef",
			offset:   0,
			encoding: "utf-16",
			want:     transport.Position{Line: 0, Character: 0},
			wantErr:  false,
		},
		{
			name:     "Offset at newline character",
			text:     "abc\ndef",
			offset:   3,
			encoding: "utf-16",
			want:     transport.Position{Line: 0, Character: 3},
			wantErr:  false,
		},
		{
			name:     "Negative offset (invalid)",
			text:     "abc\ndef",
			offset:   ^uint(0), // max uint, simulates negative for error
			encoding: "utf-16",
			want:     transport.Position{Line: 1, Character: 3},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := server.OffsetToPosition(tt.offset, tt.text, tt.encoding)
			if (err != nil) != tt.wantErr {
				t.Errorf("OffsetToPosition() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("OffsetToPosition() = %v, want %v", got, tt.want)
			}
		})
	}
}

type IncrementalTest struct {
	text string
	pos  transport.Position
	off  uint
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

func TestOffset2Position(t *testing.T) {
	testStr := `import("stdfaust.lib");
process = os.osc(400);


h = a
with {

};;
`
	//	testStr2 := "import(\"stdfaust.lib\");\nprocess = os.osc(400);\n\nh = a\n    with {\n    };\n\n"
	itests := []IncrementalTest{
		{testStr, transport.Position{0, 0}, 0},
		{testStr, transport.Position{8, 0}, uint(len(testStr))},
	}
	for _, test := range itests {
		t.Log(fmt.Sprint(testIncremental(test)))
	}
}
