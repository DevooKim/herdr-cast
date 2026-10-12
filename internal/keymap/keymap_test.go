package keymap

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const defaultConfig = `
[keys]
# prefix = "ctrl+b"
# help = "prefix+?"
# open_worktree = ""    # optional, unset by default
# remote_image_paste = "ctrl+v" # only active in herdr --remote
# switch_tab = "prefix+1..9"
# Custom commands use the same binding syntax.
# type = "shell" runs detached in the background.
# [[keys.command]]
# key = "prefix+alt+g"
# type = "popup"
# [keys.indexed]
# tabs = ""

[server]
# headless_cols = 120
`

func TestParseDefaultConfig(t *testing.T) {
	got := parseDefaultConfig([]byte(defaultConfig))
	want := map[string][]string{
		"prefix":             {"ctrl+b"},
		"help":               {"prefix+?"},
		"open_worktree":      nil,
		"remote_image_paste": {"ctrl+v"},
		"switch_tab":         {"prefix+1..9"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d keys %v, want %d", len(got), got, len(want))
	}
	for k, v := range want {
		if !slices.Equal(got[k], v) {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

func TestLoadUserConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	config := `
[keys]
prefix = ["ctrl+a", "f12"]
new_tab = "prefix+t"
help = ""
fullscreen = "prefix+f"
next_agent = ["alt+n", "prefix+shift+n"]

[[keys.command]]
key = "prefix+alt+g"
type = "popup"
command = "lazygit"
width = "80%"
height = 30

[[keys.command]]
key = "alt+i"
command = "zed ."
`
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	km := Load(path, "")
	if len(km.Diagnostics) > 0 {
		t.Fatalf("diagnostics: %v", km.Diagnostics)
	}
	if !slices.Equal(km.Prefix, []string{"ctrl+a", "f12"}) {
		t.Errorf("prefix = %v", km.Prefix)
	}
	if got := km.Label(km.Bindings["new_tab"]); got != "ctrl+a t" {
		t.Errorf("new_tab label = %q", got)
	}
	if got := km.Label(km.Bindings["next_agent"]); got != "alt+n / ctrl+a shift+n" {
		t.Errorf("next_agent label = %q", got)
	}
	if got := km.Bindings["zoom"]; !slices.Equal(got, []string{"prefix+f"}) {
		t.Errorf("zoom (via fullscreen alias) = %v", got)
	}
	if got := km.Bindings["help"]; len(got) != 0 {
		t.Errorf("help should be unset by an empty string, got %v", got)
	}
	if _, ok := km.Bindings["remote_image_paste"]; ok {
		t.Error("remote_image_paste should be ignored")
	}
	if len(km.Commands) != 2 {
		t.Fatalf("commands = %+v", km.Commands)
	}
	if c := km.Commands[0]; c.Type != "popup" || c.Width != "80%" || c.Height != "30" {
		t.Errorf("popup command = %+v", c)
	}
	if c := km.Commands[1]; c.Type != "shell" {
		t.Errorf("default command type = %q", c.Type)
	}
}
