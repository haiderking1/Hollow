package term

import "testing"

func TestAltScreenRequested(t *testing.T) {
	t.Setenv("ENOUGH_ALT_SCREEN", "")
	t.Setenv("ENOUGH_NO_FLICKER", "")
	if AltScreenRequested() {
		t.Fatal("alt screen should default off")
	}
	t.Setenv("ENOUGH_ALT_SCREEN", "1")
	if !AltScreenRequested() {
		t.Fatal("ENOUGH_ALT_SCREEN=1 should enable alt screen")
	}
	t.Setenv("ENOUGH_ALT_SCREEN", "")
	t.Setenv("ENOUGH_NO_FLICKER", "true")
	if !AltScreenRequested() {
		t.Fatal("ENOUGH_NO_FLICKER=true should enable alt screen")
	}
}
