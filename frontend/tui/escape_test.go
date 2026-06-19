package tui

import "testing"

func TestPluginsPickerEscapeDismissesFromSearch(t *testing.T) {
	app := &App{styles: NewStyles(), editor: NewEditor(512)}
	app.mode = modePluginsPicker
	app.pluginsPickerFocus = pluginsPickerFocusSearch
	app.pluginsPickerFilter = "exa"

	if !app.handlePluginsPickerKey(parsedKey{action: keyEscape}) {
		t.Fatal("expected esc handled by plugins picker")
	}
	if app.mode != modeTask {
		t.Fatalf("expected modeTask, got %d", app.mode)
	}
}

func TestHandleKeyEscapeFromPluginsSecret(t *testing.T) {
	app := &App{styles: NewStyles(), editor: NewEditor(512)}
	app.mode = modePluginsSecret
	app.pluginsPendingEntryID = "exa"

	app.handleKey(parsedKey{action: keyEscape})
	if app.mode != modeTask {
		t.Fatalf("expected modeTask, got %d", app.mode)
	}
	if app.pluginsPendingEntryID != "" {
		t.Fatal("expected pending entry cleared")
	}
}

func TestHandleKeyEscapeDismissesSlashMenu(t *testing.T) {
	app := &App{styles: NewStyles(), editor: NewEditor(512)}
	app.mode = modeTask
	app.editor.SetValue("/plug")

	app.handleKey(parsedKey{action: keyEscape})
	if app.editor.Value() != "" {
		t.Fatalf("expected slash menu dismissed, editor %q", app.editor.Value())
	}
}

func TestHandleKeyEscapeFromPluginsPicker(t *testing.T) {
	app := &App{styles: NewStyles(), editor: NewEditor(512)}
	app.mode = modePluginsPicker
	app.pluginsPickerFocus = pluginsPickerFocusList

	app.handleKey(parsedKey{action: keyEscape})
	if app.mode != modeTask {
		t.Fatalf("expected modeTask, got %d", app.mode)
	}
}
