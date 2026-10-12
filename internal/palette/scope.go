package palette

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The last scope picked with tab/shift+tab is remembered across palette
// openings in the plugin's state directory. Without HERDR_PLUGIN_STATE_DIR
// (tests, `herdr-cast list`) nothing is read or written.
const scopeFile = "scope"

func scopePath() string {
	dir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, scopeFile)
}

func loadScope() Category {
	path := scopePath()
	if path == "" {
		return CatAll
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return CatAll
	}
	name := strings.TrimSpace(string(raw))
	if i := slices.IndexFunc(categories, func(c Category) bool { return c.String() == name }); i >= 0 {
		return categories[i]
	}
	return CatAll
}

func saveScope(c Category) {
	if path := scopePath(); path != "" {
		_ = os.WriteFile(path, []byte(c.String()+"\n"), 0o644)
	}
}
