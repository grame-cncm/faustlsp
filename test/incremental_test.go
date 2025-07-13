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

func TestApplyIncrementalChange(t *testing.T) {
	tests := []struct {
		name        string
		original    string
		changeRange transport.Range
		newText     string
		encoding    string
		want        string
	}{
		{
			name:        "Replace middle of line",
			original:    "abcdef",
			changeRange: transport.Range{Start: transport.Position{Line: 0, Character: 2}, End: transport.Position{Line: 0, Character: 4}},
			newText:     "XY",
			encoding:    "utf-16",
			want:        "abXYef",
		},
		{
			name:        "Insert at start",
			original:    "abcdef",
			changeRange: transport.Range{Start: transport.Position{Line: 0, Character: 0}, End: transport.Position{Line: 0, Character: 0}},
			newText:     "123",
			encoding:    "utf-16",
			want:        "123abcdef",
		},
		{
			name:        "Insert at end",
			original:    "abcdef",
			changeRange: transport.Range{Start: transport.Position{Line: 0, Character: 6}, End: transport.Position{Line: 0, Character: 6}},
			newText:     "XYZ",
			encoding:    "utf-16",
			want:        "abcdefXYZ",
		},
		{
			name:        "Delete range",
			original:    "abcdef",
			changeRange: transport.Range{Start: transport.Position{Line: 0, Character: 2}, End: transport.Position{Line: 0, Character: 5}},
			newText:     "",
			encoding:    "utf-16",
			want:        "abf",
		},
		{
			name:        "Replace whole document",
			original:    "abcdef",
			changeRange: transport.Range{Start: transport.Position{Line: 0, Character: 0}, End: transport.Position{Line: 0, Character: 6}},
			newText:     "xyz",
			encoding:    "utf-16",
			want:        "xyz",
		},
		{
			name:        "Undo: revert to previous state",
			original:    "abXYef",
			changeRange: transport.Range{Start: transport.Position{Line: 0, Character: 2}, End: transport.Position{Line: 0, Character: 4}},
			newText:     "cd",
			encoding:    "utf-16",
			want:        "abcdef",
		},
		{
			name:        "Out of bounds range (end too large)",
			original:    "abcdef",
			changeRange: transport.Range{Start: transport.Position{Line: 0, Character: 4}, End: transport.Position{Line: 0, Character: 100}},
			newText:     "ZZ",
			encoding:    "utf-16",
			want:        "abcdZZ",
		},
		{
			name:        "Multi-line replace",
			original:    "abc\ndef\nghi",
			changeRange: transport.Range{Start: transport.Position{Line: 1, Character: 0}, End: transport.Position{Line: 2, Character: 3}},
			newText:     "XYZ",
			encoding:    "utf-16",
			want:        "abc\nXYZ",
		},
		{
			name:        "Insert newline",
			original:    "abc\ndef",
			changeRange: transport.Range{Start: transport.Position{Line: 0, Character: 3}, End: transport.Position{Line: 0, Character: 3}},
			newText:     "\n",
			encoding:    "utf-16",
			want:        "abc\n\ndef",
		},
		{
			name:        "Undo: insert then remove",
			original:    "abc\n\ndef",
			changeRange: transport.Range{Start: transport.Position{Line: 0, Character: 3}, End: transport.Position{Line: 1, Character: 0}},
			newText:     "",
			encoding:    "utf-16",
			want:        "abc\ndef",
		},
		{
			name:        "Insert at empty document",
			original:    "",
			changeRange: transport.Range{Start: transport.Position{Line: 0, Character: 0}, End: transport.Position{Line: 0, Character: 0}},
			newText:     "hello",
			encoding:    "utf-16",
			want:        "hello",
		},
		{
			name:        "Replace with empty string (delete all)",
			original:    "abcdef",
			changeRange: transport.Range{Start: transport.Position{Line: 0, Character: 0}, End: transport.Position{Line: 0, Character: 6}},
			newText:     "",
			encoding:    "utf-16",
			want:        "",
		},
		{
			name:        "Insert at end of multi-line document",
			original:    "abc\ndef\nghi",
			changeRange: transport.Range{Start: transport.Position{Line: 2, Character: 3}, End: transport.Position{Line: 2, Character: 3}},
			newText:     "XYZ",
			encoding:    "utf-16",
			want:        "abc\ndef\nghiXYZ",
		},
		{
			name:        "Replace across multiple lines with longer text",
			original:    "abc\ndef\nghi",
			changeRange: transport.Range{Start: transport.Position{Line: 0, Character: 1}, End: transport.Position{Line: 2, Character: 2}},
			newText:     "LONGREPLACEMENT",
			encoding:    "utf-16",
			want:        "aLONGREPLACEMENTi",
		},
		{
			name:        "Insert unicode emoji",
			original:    "abc",
			changeRange: transport.Range{Start: transport.Position{Line: 0, Character: 1}, End: transport.Position{Line: 0, Character: 1}},
			newText:     "💚",
			encoding:    "utf-16",
			want:        "a💚bc",
		},
		{
			name:        "Replace with only newlines",
			original:    "abc\ndef",
			changeRange: transport.Range{Start: transport.Position{Line: 0, Character: 0}, End: transport.Position{Line: 1, Character: 3}},
			newText:     "\n\n\n",
			encoding:    "utf-16",
			want:        "\n\n\n",
		},
		{
			name:        "Insert at very large character index",
			original:    "abc",
			changeRange: transport.Range{Start: transport.Position{Line: 0, Character: 1000}, End: transport.Position{Line: 0, Character: 1000}},
			newText:     "XYZ",
			encoding:    "utf-16",
			want:        "abcXYZ",
		},
		{
			name:        "Replace with multi-line text",
			original:    "abc\ndef",
			changeRange: transport.Range{Start: transport.Position{Line: 0, Character: 1}, End: transport.Position{Line: 1, Character: 2}},
			newText:     "1\n2\n3",
			encoding:    "utf-16",
			want:        "a1\n2\n3f",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := server.ApplyIncrementalChange(tt.changeRange, tt.newText, tt.original, tt.encoding)
			if got != tt.want {
				t.Errorf("ApplyIncrementalChange() = %q, want %q", got, tt.want)
			}
		})
	}
}
