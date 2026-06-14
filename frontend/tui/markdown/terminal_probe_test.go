package markdown

import "testing"

func TestHandleTerminalResponseCellSize(t *testing.T) {
	seq := []byte("\x1b[6;39;18t")
	if !HandleTerminalResponse(seq) {
		t.Fatal("expected cell size report to be consumed")
	}
	if got := GetCellDimensions(); got.WidthPx != 18 || got.HeightPx != 39 {
		t.Fatalf("unexpected cell dims: %+v", got)
	}
	if HandleTerminalResponse([]byte("\x1b[A")) {
		t.Fatal("expected key sequence to pass through")
	}
}
