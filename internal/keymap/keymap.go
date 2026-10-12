// Package keymap resolves Herdr's effective keybindings: built-in defaults,
// the defaults printed by the installed binary, and the user's config.toml.
package keymap

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// Defaults for Herdr 0.9.3. `herdr --default-config` overrides these at
// runtime, so newer binaries win; it omits a few keys (swap_pane_*, copy_mode),
// which is why this table exists at all.
var builtinDefaults = map[string]string{
	"prefix":                   "ctrl+b",
	"help":                     "prefix+?",
	"settings":                 "prefix+s",
	"detach":                   "prefix+q",
	"reload_config":            "prefix+shift+r",
	"open_notification_target": "prefix+o",
	"workspace_picker":         "prefix+w",
	"goto":                     "prefix+g",
	"new_workspace":            "prefix+shift+n",
	"new_worktree":             "prefix+shift+g",
	"rename_workspace":         "prefix+shift+w",
	"close_workspace":          "prefix+shift+d",
	"new_tab":                  "prefix+c",
	"rename_tab":               "prefix+shift+t",
	"previous_tab":             "prefix+p",
	"next_tab":                 "prefix+n",
	"switch_tab":               "prefix+1..9",
	"close_tab":                "prefix+shift+x",
	"rename_pane":              "prefix+shift+p",
	"edit_scrollback":          "prefix+e",
	"copy_mode":                "prefix+[",
	"focus_pane_left":          "prefix+h",
	"focus_pane_down":          "prefix+j",
	"focus_pane_up":            "prefix+k",
	"focus_pane_right":         "prefix+l",
	"swap_pane_left":           "prefix+shift+h",
	"swap_pane_down":           "prefix+shift+j",
	"swap_pane_up":             "prefix+shift+k",
	"swap_pane_right":          "prefix+shift+l",
	"cycle_pane_next":          "prefix+tab",
	"cycle_pane_previous":      "prefix+shift+tab",
	"split_vertical":           "prefix+v",
	"split_horizontal":         "prefix+minus",
	"close_pane":               "prefix+x",
	"zoom":                     "prefix+z",
	"resize_mode":              "prefix+r",
	"toggle_sidebar":           "prefix+b",
}

// Keys that are not palette material: navigate-mode movement is modal-local
// and remote_image_paste only exists under `herdr --remote`.
var ignoredKeys = map[string]bool{
	"navigate_workspace_up":   true,
	"navigate_workspace_down": true,
	"navigate_pane_left":      true,
	"navigate_pane_down":      true,
	"navigate_pane_up":        true,
	"navigate_pane_right":     true,
	"remote_image_paste":      true,
}

var keyAliases = map[string]string{"fullscreen": "zoom"}

// CustomCommand is one [[keys.command]] entry.
type CustomCommand struct {
	Keys        []string
	Type        string // shell | pane | popup | plugin_action
	Command     string
	Description string
	Width       string
	Height      string
}

// Keymap is the effective keybinding set.
type Keymap struct {
	Prefix   []string
	Bindings map[string][]string // action name -> raw bindings ("prefix+c")
	Commands []CustomCommand
	// AgentPanelSort is [ui] agent_panel_sort ("spaces" or "priority"), which
	// decides the order previous_agent/next_agent walk.
	AgentPanelSort string
	// Diagnostics collects non-fatal problems, e.g. an unreadable config.
	Diagnostics []string
}

// Load resolves the effective keymap. herdrBin may be empty to skip asking the
// installed binary for its defaults.
func Load(configPath, herdrBin string) *Keymap {
	km := &Keymap{Bindings: map[string][]string{}}
	for name, value := range builtinDefaults {
		km.Bindings[name] = splitBinding(value)
	}
	if herdrBin != "" {
		if out, err := exec.Command(herdrBin, "--default-config").Output(); err == nil {
			for name, value := range parseDefaultConfig(out) {
				km.Bindings[name] = value
			}
		}
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			km.Diagnostics = append(km.Diagnostics, fmt.Sprintf("config read error: %v", err))
		}
	} else if err := km.applyConfig(raw); err != nil {
		km.Diagnostics = append(km.Diagnostics, fmt.Sprintf("config parse error: %v", err))
	}

	km.Prefix = km.Bindings["prefix"]
	delete(km.Bindings, "prefix")
	for name := range ignoredKeys {
		delete(km.Bindings, name)
	}
	return km
}

// Matches both `prefix = "ctrl+b"` and the commented `# help = "prefix+?"`,
// but not prose such as `# type = "shell" runs detached in the background.`
var defaultLine = regexp.MustCompile(`^#?\s*([a-z_]+)\s*=\s*("[^"]*"|\[[^\]]*\])\s*(#.*)?$`)

// parseDefaultConfig reads the `name = "value"` lines (commented or not)
// inside the [keys] section of `herdr --default-config`.
func parseDefaultConfig(out []byte) map[string][]string {
	found := map[string][]string{}
	inKeys := false
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Commented headers such as `# [[keys.command]]` end the keys block
		// too; their example fields are not bindings.
		if header := strings.TrimSpace(strings.TrimPrefix(line, "#")); strings.HasPrefix(header, "[") {
			inKeys = line == "[keys]"
			continue
		}
		if !inKeys {
			continue
		}
		if m := defaultLine.FindStringSubmatch(line); m != nil {
			found[m[1]] = decodeValue(m[2])
		}
	}
	return found
}

func decodeValue(literal string) []string {
	var doc struct{ V any }
	if _, err := toml.Decode("V = "+literal, &doc); err != nil {
		return nil
	}
	return bindingValue(doc.V)
}

func (km *Keymap) applyConfig(raw []byte) error {
	var doc struct {
		Keys map[string]any `toml:"keys"`
		UI   struct {
			AgentPanelSort string `toml:"agent_panel_sort"`
		} `toml:"ui"`
	}
	if _, err := toml.Decode(string(raw), &doc); err != nil {
		return err
	}
	km.AgentPanelSort = doc.UI.AgentPanelSort
	for name, value := range doc.Keys {
		if canonical, ok := keyAliases[name]; ok {
			if _, both := doc.Keys[canonical]; both {
				continue // the canonical key wins over its legacy alias
			}
			name = canonical
		}
		switch name {
		case "command":
			km.Commands = parseCommands(value)
		case "indexed":
			km.applyIndexed(value)
		default:
			switch value.(type) {
			case string, []any:
				km.Bindings[name] = bindingValue(value)
			}
		}
	}
	return nil
}

// applyIndexed adds the legacy [keys.indexed] modifiers (e.g. tabs = "ctrl")
// as extra "<mod>+1..9" bindings on the matching indexed actions.
func (km *Keymap) applyIndexed(value any) {
	table, ok := value.(map[string]any)
	if !ok {
		return
	}
	legacy := map[string]string{"tabs": "switch_tab", "workspaces": "switch_workspace", "agents": "focus_agent"}
	for field, action := range legacy {
		if mod, _ := table[field].(string); mod != "" {
			km.Bindings[action] = append(km.Bindings[action], mod+"+1..9")
		}
	}
}

func parseCommands(value any) []CustomCommand {
	entries, ok := value.([]map[string]any)
	if !ok {
		if list, isList := value.([]any); isList {
			for _, item := range list {
				if m, ok := item.(map[string]any); ok {
					entries = append(entries, m)
				}
			}
		}
	}
	var commands []CustomCommand
	for _, entry := range entries {
		cmd := CustomCommand{
			Keys:        bindingValue(entry["key"]),
			Type:        stringValue(entry["type"]),
			Command:     stringValue(entry["command"]),
			Description: stringValue(entry["description"]),
			Width:       sizeValue(entry["width"]),
			Height:      sizeValue(entry["height"]),
		}
		if cmd.Type == "" {
			cmd.Type = "shell"
		}
		if strings.TrimSpace(cmd.Command) == "" {
			continue
		}
		commands = append(commands, cmd)
	}
	return commands
}

func isList(value any) bool {
	_, ok := value.([]any)
	return ok
}

func bindingValue(value any) []string {
	switch v := value.(type) {
	case string:
		return splitBinding(v)
	case []any:
		var out []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, splitBinding(s)...)
			}
		}
		return out
	}
	return nil
}

func splitBinding(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return []string{value}
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}

func sizeValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case int64:
		return fmt.Sprint(v)
	case float64:
		return fmt.Sprint(int(v))
	}
	return ""
}

// Label renders raw bindings for display, e.g. "prefix+shift+n" with prefix
// "ctrl+b" becomes "ctrl+b shift+n". Several bindings are joined with " / ".
func (km *Keymap) Label(bindings []string) string {
	prefix := "prefix"
	if len(km.Prefix) > 0 {
		prefix = km.Prefix[0]
	}
	parts := make([]string, 0, len(bindings))
	for _, b := range bindings {
		if rest, ok := strings.CutPrefix(b, "prefix+"); ok {
			parts = append(parts, prefix+" "+rest)
		} else {
			parts = append(parts, b)
		}
	}
	return strings.Join(parts, " / ")
}
