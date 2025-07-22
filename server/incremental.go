package server

import (
	"fmt"
	"unicode/utf8"

	"github.com/carn181/faustlsp/transport"
)

func ApplyIncrementalChange(r transport.Range, newContent string, content string, encoding string) string {
	start, _ := PositionToOffset(r.Start, content, encoding)
	end, _ := PositionToOffset(r.End, content, encoding)
	//	logging.Logger.Printf("Start: %d, End: %d\n", start, end)
	return content[:start] + newContent + content[end:]
}

func PositionToOffset(pos transport.Position, s string, encoding string) (uint, error) {
	if len(s) == 0 {
		return 0, nil
	}
	indices := GetLineIndices(s)
	if pos.Line > uint32(len(indices)) {
		return 0, fmt.Errorf("invalid Line Number")
	} else if pos.Line == uint32(len(indices)) {
		return uint(len(s)), nil
	}
	currChar := indices[pos.Line]
	for i := 0; i < int(pos.Character); i++ {
		if int(currChar) >= len(s) {
			break // Prevent reading past end of string
		}
		r, w := utf8.DecodeRuneInString(s[currChar:])
		if w == 0 {
			break // Prevent infinite loop if decoding fails
		}
		currChar += uint(w)
		if encoding == "utf-16" {
			if r >= 0x10000 {
				i++
				if i == int(pos.Character) {
					break
				}
			}
		}
	}
	return currChar, nil
}

func OffsetToPosition(offset uint, s string, encoding string) (transport.Position, error) {
	if len(s) == 0 || offset == 0 {
		return transport.Position{Line: 0, Character: 0}, nil
	}
	line := uint32(0)
	char := uint32(0)
	str := []byte(s)

	for i := uint(0); i < offset && i < uint(len(str)); {
		r, w := utf8.DecodeRune(str[i:])
		if w == 0 {
			break // Prevent infinite loop if decoding fails
		}
		if r == '\n' {
			line++
			char = 0
		} else {
			char++
			if r >= 0x10000 && encoding == "utf-16" {
				char++
			}
		}
		i += uint(w)
	}

	return transport.Position{Line: line, Character: char}, nil
}

func GetLineIndices(s string) []uint {
	//	logging.Logger.Printf("Got %s\n", s)
	lines := []uint{0}
	i := 0
	for w := 0; i < len(s); i += w {
		runeValue, width := utf8.DecodeRuneInString(s[i:])
		if runeValue == '\n' {
			lines = append(lines, uint(i)+1)
		}
		w = width
	}
	return lines
}

func getDocumentEndOffset(s string, encoding string) uint {
	switch encoding {
	case "utf-8":
		return uint(len(s))
	case "utf-16":
		offset := uint(0)
		for _, r := range s {
			if r >= 0x10000 {
				offset += 2
			} else {
				offset += 1
			}
		}
		return offset
	case "utf-32":
		// Each rune is one code unit in utf-32
		return uint(len([]rune(s)))
	default:
		// Fallback to utf-8
		return uint(len(s))
	}
}

func getDocumentEndPosition(s string, encoding string) (transport.Position, error) {
	offset := getDocumentEndOffset(s, encoding)
	pos, err := OffsetToPosition(offset, s, encoding)
	return pos, err
}
