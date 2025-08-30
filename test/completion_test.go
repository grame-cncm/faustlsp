package tests

import (
	"testing"

	"github.com/carn181/faustlsp/logging"
	"github.com/carn181/faustlsp/server"
	"github.com/carn181/faustlsp/transport"
)

func TestFindCompletionReplaceRange(t *testing.T) {
	logging.Init()

	tests := []struct {
		name     string
		text     string
		position transport.Position
		encoding string
		want     transport.Range
	}{
		{
			name: "Simple prefix",
			text: `import("stdfaust.lib");
foo = 1;`,
			position: transport.Position{Line: 1, Character: 2},
			encoding: "utf-8",
			want: transport.Range{
				Start: transport.Position{Line: 1, Character: 0},
				End:   transport.Position{Line: 1, Character: 2},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := server.FindCompletionReplaceRange(tt.position, tt.text, tt.encoding); got != tt.want {
				t.Errorf("FindCompletionReplaceRange() = %v, want %v", got, tt.want)
			}
		})
	}
}
