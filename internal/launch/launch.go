// Package launch starts work that must outlive the palette popup.
//
// Herdr allows one popup at a time and closes it when the palette process
// exits. Anything that opens Herdr UI of its own (another popup, an overlay
// pane, a plugin action that may do either) therefore runs as a Job in a
// detached `herdr-cast exec` process that waits for the palette to exit first.
package launch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/DevooKim/herdr-cast/internal/herdr"
)

const jobEnv = "HERDR_CAST_JOB"

// Runner pane entrypoint declared in herdr-plugin.toml. It runs
// $HERDR_CAST_COMMAND through /bin/sh -c, like Herdr's own pane commands.
const runnerEntrypoint = "run"

type Job struct {
	Kind     string            `json:"kind"` // plugin_action | pane | popup | api
	PluginID string            `json:"plugin_id,omitempty"`
	ActionID string            `json:"action_id,omitempty"`
	Command  string            `json:"command,omitempty"`
	Cwd      string            `json:"cwd,omitempty"`
	Env      map[string]string `json:"env,omitempty"`
	Width    string            `json:"width,omitempty"`
	Height   string            `json:"height,omitempty"`
	Method   string            `json:"method,omitempty"`
	Params   map[string]any    `json:"params,omitempty"`
	Title    string            `json:"title,omitempty"`
	WaitPID  int               `json:"wait_pid"`
}

// Defer starts `herdr-cast exec` detached from the popup's terminal session
// so it survives the popup closing, and hands it the job.
func Defer(job Job) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	job.WaitPID = os.Getpid()
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	cmd := exec.Command(self, "exec")
	cmd.Env = append(os.Environ(), jobEnv+"="+string(payload))
	return startDetached(cmd, logFile())
}

// Shell runs a `type = "shell"` custom command the way Herdr does: detached,
// through `/bin/sh -lc`, in the focused pane's directory.
func Shell(command, cwd string, env map[string]string) error {
	cmd := exec.Command("/bin/sh", "-lc", command)
	if isDir(cwd) {
		cmd.Dir = cwd
	}
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return startDetached(cmd, "")
}

func startDetached(cmd *exec.Cmd, logPath string) error {
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devnull.Close()
	cmd.Stdin = devnull
	cmd.Stdout = devnull
	cmd.Stderr = devnull
	if logPath != "" {
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			defer f.Close()
			cmd.Stdout = f
			cmd.Stderr = f
		}
	}
	// A new session drops the popup's controlling terminal, so the child is
	// not hung up when Herdr tears the popup down.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func logFile() string {
	dir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "exec.log")
}

// RunFromEnv is the body of `herdr-cast exec`.
func RunFromEnv() error {
	var job Job
	if err := json.Unmarshal([]byte(os.Getenv(jobEnv)), &job); err != nil {
		return fmt.Errorf("decode %s: %w", jobEnv, err)
	}
	client := herdr.NewClient()
	err := run(client, job)
	if err != nil {
		title := "herdr-cast"
		if job.Title != "" {
			title = "herdr-cast: " + job.Title
		}
		_ = client.Call("notification.show", map[string]any{"title": title, "body": err.Error()}, nil)
		fmt.Fprintf(os.Stderr, "%s %s: %v\n", time.Now().Format(time.RFC3339), job.Kind, err)
	}
	return err
}

func run(client *herdr.Client, job Job) error {
	waitForExit(job.WaitPID, 3*time.Second)
	// The server reaps the popup asynchronously after the palette exits; close
	// it explicitly so the next popup does not race it.
	_ = client.Call("popup.close", nil, nil)

	switch job.Kind {
	case "plugin_action":
		return retryBusy(func() error {
			return client.Call("plugin.action.invoke", map[string]any{
				"action_id": job.ActionID,
				"context":   map[string]any{"invocation_source": "herdr-cast"},
			}, nil)
		})
	case "pane", "popup":
		params := map[string]any{
			"plugin_id":  job.PluginID,
			"entrypoint": runnerEntrypoint,
			"placement":  "overlay",
			"focus":      true,
			"env":        withCommand(job.Env, job.Command),
		}
		if isDir(job.Cwd) {
			params["cwd"] = job.Cwd
		}
		if job.Kind == "popup" {
			params["placement"] = "popup"
			if size, _ := herdr.PopupSize(job.Width); size != nil {
				params["width"] = size
			}
			if size, _ := herdr.PopupSize(job.Height); size != nil {
				params["height"] = size
			}
		}
		return retryBusy(func() error { return client.Call("plugin.pane.open", params, nil) })
	case "api":
		return retryBusy(func() error { return client.Call(job.Method, job.Params, nil) })
	}
	return fmt.Errorf("unknown job kind %q", job.Kind)
}

func withCommand(env map[string]string, command string) map[string]string {
	out := map[string]string{"HERDR_CAST_COMMAND": command}
	for k, v := range env {
		out[k] = v
	}
	return out
}

func retryBusy(call func() error) error {
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := call()
		if err == nil || !herdr.IsCode(err, "ui_busy") || time.Now().After(deadline) {
			return err
		}
		time.Sleep(40 * time.Millisecond)
	}
}

func waitForExit(pid int, limit time.Duration) {
	if pid <= 0 {
		return
	}
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func isDir(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
