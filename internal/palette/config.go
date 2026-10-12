package palette

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/BurntSushi/toml"

	"github.com/DevooKim/herdr-cast/internal/herdr"
)

// Config is herdr-cast's own settings file, config.toml in the plugin's
// config directory (`herdr plugin config-dir devookim.herdr-cast`).
type Config struct {
	// DisabledScopes hides whole categories, by scope id:
	// agents, workspaces, tabs, plugin_actions, commands, keybindings.
	DisabledScopes []string `toml:"disabled_scopes"`

	// Width and Height size the palette popup: a percentage string such as
	// "80%" or a number of terminal cells. Unset keeps the manifest default.
	Width  any `toml:"width"`
	Height any `toml:"height"`
}

func (c Category) ID() string {
	return [...]string{"all", "agents", "workspaces", "tabs", "plugin_actions", "commands", "keybindings"}[c]
}

// LoadConfig reads config.toml from dir. A missing file is the default
// config; problems come back as warnings and never stop the palette.
func LoadConfig(dir string) (Config, []string) {
	var cfg Config
	if dir == "" {
		return cfg, nil
	}
	path := filepath.Join(dir, "config.toml")
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, []string{fmt.Sprintf("herdr-cast config: %v", err)}
	}
	if _, err := toml.Decode(string(raw), &cfg); err != nil {
		return Config{}, []string{fmt.Sprintf("herdr-cast config: %v", err)}
	}
	var warnings []string
	for _, id := range cfg.DisabledScopes {
		if !slices.ContainsFunc(categories[1:], func(c Category) bool { return c.ID() == id }) {
			warnings = append(warnings, fmt.Sprintf("herdr-cast config: unknown scope %q", id))
		}
	}
	for _, size := range []struct {
		name  string
		value *any
	}{{"width", &cfg.Width}, {"height", &cfg.Height}} {
		if _, err := herdr.PopupSize(sizeString(*size.value)); err != nil {
			warnings = append(warnings, fmt.Sprintf("herdr-cast config: %s: %v", size.name, err))
			*size.value = nil
		}
	}
	return cfg, warnings
}

// PopupParams returns the width/height fields for plugin.pane.open; sizes
// that are unset or invalid are left out so the manifest default applies.
func (cfg Config) PopupParams() map[string]any {
	params := map[string]any{}
	if size, _ := herdr.PopupSize(sizeString(cfg.Width)); size != nil {
		params["width"] = size
	}
	if size, _ := herdr.PopupSize(sizeString(cfg.Height)); size != nil {
		params["height"] = size
	}
	return params
}

// sizeString accepts TOML's string or integer form of a size.
func sizeString(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case int64:
		return strconv.FormatInt(v, 10)
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

// Hidden reports whether the config disables category c. "All" is always shown.
func (cfg Config) Hidden(c Category) bool {
	return c != CatAll && slices.Contains(cfg.DisabledScopes, c.ID())
}

// scopes lists the categories the scope bar and tab cycling offer.
func (cfg Config) scopes() []Category {
	return slices.DeleteFunc(slices.Clone(categories), cfg.Hidden)
}
