// Package herdr talks to a running Herdr server over its newline-delimited
// JSON socket API.
package herdr

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// Error is an error response returned by the Herdr server.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Message
}

// IsCode reports whether err is a server error with the given code.
func IsCode(err error, code string) bool {
	herr, ok := err.(*Error)
	return ok && herr.Code == code
}

// Client sends requests to one Herdr socket. Every call uses its own
// connection, so a Client is safe for concurrent use.
type Client struct {
	SocketPath string
	Timeout    time.Duration
}

var requestSeq atomic.Uint64

// NewClient resolves the socket the same way the Herdr CLI does for plugin
// processes: HERDR_SOCKET_PATH first, then the default session socket.
func NewClient() *Client {
	path := os.Getenv("HERDR_SOCKET_PATH")
	if path == "" {
		path = filepath.Join(ConfigDir(), "herdr.sock")
	}
	return &Client{SocketPath: path, Timeout: 5 * time.Second}
}

// ConfigDir mirrors Herdr's config directory resolution.
func ConfigDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "herdr")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "herdr")
	}
	return filepath.Join(home, ".config", "herdr")
}

// ConfigPath mirrors Herdr's config.toml resolution.
func ConfigPath() string {
	if path := os.Getenv("HERDR_CONFIG_PATH"); path != "" {
		return path
	}
	return filepath.Join(ConfigDir(), "config.toml")
}

// Call sends one request and decodes its result into out (which may be nil).
func (c *Client) Call(method string, params any, out any) error {
	if params == nil {
		params = struct{}{}
	}
	conn, err := net.DialTimeout("unix", c.SocketPath, c.Timeout)
	if err != nil {
		return fmt.Errorf("connect to herdr (%s): %w", c.SocketPath, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.Timeout))

	id := fmt.Sprintf("cast_%d", requestSeq.Add(1))
	req, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}

	reader := bufio.NewReaderSize(conn, 64*1024)
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return fmt.Errorf("%s: %w", method, err)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *Error          `json:"error"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("%s: decode response: %w", method, err)
	}
	if resp.Error != nil {
		return resp.Error
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(resp.Result, out)
}
