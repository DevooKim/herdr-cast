// Command herdr-cast is a Spotlight/Raycast-style command palette for Herdr.
//
//	herdr-cast open           open the palette popup, sized from config.toml
//	herdr-cast palette        run the palette UI (the plugin's popup pane)
//	herdr-cast exec           run a deferred job after the palette closes
//	herdr-cast list [query]   print palette rows without the UI, for debugging
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DevooKim/herdr-cast/internal/herdr"
	"github.com/DevooKim/herdr-cast/internal/keymap"
	"github.com/DevooKim/herdr-cast/internal/launch"
	"github.com/DevooKim/herdr-cast/internal/palette"
)

const defaultPluginID = "devookim.herdr-cast"

func main() {
	cmd := "palette"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "open":
		err = runOpen()
	case "palette":
		err = runPalette()
	case "exec":
		err = launch.RunFromEnv()
	case "list":
		err = runList(strings.Join(os.Args[2:], " "))
	default:
		err = fmt.Errorf("unknown command %q (want open, palette, exec, or list)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "herdr-cast:", err)
		os.Exit(1)
	}
}

func herdrBin() string {
	if bin := os.Getenv("HERDR_BIN_PATH"); bin != "" {
		return bin
	}
	bin, _ := exec.LookPath("herdr")
	return bin
}

// pluginConfig reads herdr-cast's config.toml. Outside a plugin process
// (`herdr-cast list`) it falls back to Herdr's default plugin config path.
func pluginConfig() (palette.Config, []string) {
	dir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR")
	if dir == "" {
		dir = filepath.Join(herdr.ConfigDir(), "plugins", "config", pluginID())
	}
	return palette.LoadConfig(dir)
}

func pluginID() string {
	if id := os.Getenv("HERDR_PLUGIN_ID"); id != "" {
		return id
	}
	return defaultPluginID
}

func load(cfg palette.Config, cfgWarnings []string) (*palette.Env, []palette.Item, []string, error) {
	client := herdr.NewClient()
	bin := herdrBin()

	var (
		wg         sync.WaitGroup
		snap       *herdr.Snapshot
		snapErr    error
		agents     []herdr.Agent
		agentsErr  error
		plugins    []herdr.Plugin
		pluginsErr error
		km         *keymap.Keymap
	)
	wg.Add(4)
	go func() { defer wg.Done(); snap, snapErr = client.Snapshot() }()
	go func() { defer wg.Done(); agents, agentsErr = client.Agents() }()
	go func() { defer wg.Done(); plugins, pluginsErr = client.Plugins() }()
	go func() { defer wg.Done(); km = keymap.Load(herdr.ConfigPath(), bin) }()
	wg.Wait()

	if snapErr != nil {
		return nil, nil, nil, snapErr
	}
	if agentsErr == nil {
		snap.Agents = agents
	}
	warnings := slices.Concat(cfgWarnings, km.Diagnostics)
	if pluginsErr != nil {
		warnings = append(warnings, "plugin list: "+pluginsErr.Error())
	}
	env := &palette.Env{
		Client:   client,
		Snap:     snap,
		Ctx:      herdr.ContextFromEnv(),
		Keymap:   km,
		PluginID: pluginID(),
	}
	return env, palette.BuildItems(env, plugins, cfg), warnings, nil
}

// runOpen is the plugin's open action. Herdr only reads popup sizes from the
// manifest or the open request, so config.toml sizes are passed here.
func runOpen() error {
	cfg, warnings := pluginConfig()
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, w) // recorded in `herdr plugin log list`
	}
	params := cfg.PopupParams()
	params["plugin_id"] = pluginID()
	params["entrypoint"] = "palette"
	return herdr.NewClient().Call("plugin.pane.open", params, nil)
}

func runPalette() error {
	cfg, warnings := pluginConfig()
	loader := func() (*palette.Env, []palette.Item, []string, error) { return load(cfg, warnings) }
	_, err := tea.NewProgram(palette.NewModel(loader, cfg), tea.WithAltScreen()).Run()
	return err
}

func runList(query string) error {
	_, items, warnings, err := load(pluginConfig())
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	for _, item := range palette.Filter(items, palette.CatAll, query) {
		current := ""
		if item.Current {
			current = " *"
		}
		fmt.Printf("%-7s %-8s %-40s %-30s %s%s\n", item.Cat.Badge(), item.Status, item.Title, item.Key, item.Detail, current)
	}
	return nil
}
