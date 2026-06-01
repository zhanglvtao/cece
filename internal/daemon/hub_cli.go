package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"
)

// HubClient is a client that talks to the hub daemon over Unix socket.
type HubClient struct {
	socketPath string
}

// NewHubClient creates a client connected to the hub socket.
func NewHubClient() *HubClient {
	return &HubClient{
		socketPath: defaultSocketPath(),
	}
}

func defaultSocketPath() string {
	home, _ := os.UserHomeDir()
	if home != "" {
		p := home + "/.cece/hub.sock"
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Fallback: project-local
	dir, _ := os.Getwd()
	return dir + "/.cece/hub.sock"
}

func (c *HubClient) call(method string, params any) (*HubResponse, error) {
	var paramsJSON json.RawMessage
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		paramsJSON = data
	}

	req := HubRequest{
		Method: method,
		Params: paramsJSON,
	}
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return nil, fmt.Errorf("connect to hub: %w", err)
	}
	defer conn.Close()

	if _, err := conn.Write(append(reqData, '\n')); err != nil {
		return nil, fmt.Errorf("send to hub: %w", err)
	}

	scanner := bufio.NewScanner(conn)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 8*1024*1024)

	if !scanner.Scan() {
		return nil, fmt.Errorf("no response from hub")
	}

	var resp HubResponse
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("parse hub response: %w", err)
	}

	return &resp, nil
}

// ListSessions returns all sessions tracked by the hub.
func (c *HubClient) ListSessions() ([]*ManagedSession, error) {
	resp, err := c.call("session.list", nil)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf(resp.Error)
	}

	var sessions []*ManagedSession
	if err := json.Unmarshal(resp.Result, &sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

// GetSession returns a single session by ID.
func (c *HubClient) GetSession(id string) (*ManagedSession, error) {
	resp, err := c.call("session.get", sessionIDParams{SessionID: id})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf(resp.Error)
	}

	var ms ManagedSession
	if err := json.Unmarshal(resp.Result, &ms); err != nil {
		return nil, err
	}
	return &ms, nil
}

// CreateSession creates a new session with a hub-managed engine.
func (c *HubClient) CreateSession() (*ManagedSession, error) {
	resp, err := c.call("session.create", nil)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf(resp.Error)
	}

	var ms ManagedSession
	if err := json.Unmarshal(resp.Result, &ms); err != nil {
		return nil, err
	}
	return &ms, nil
}

// DeleteSession deletes a session and stops its engine if running.
func (c *HubClient) DeleteSession(id string) error {
	resp, err := c.call("session.delete", sessionIDParams{SessionID: id})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf(resp.Error)
	}
	return nil
}

// SendInput sends a text input to a session's engine.
func (c *HubClient) SendInput(id, text string) error {
	resp, err := c.call("session.input", sessionInputParams{SessionID: id, Text: text})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf(resp.Error)
	}
	return nil
}

// CancelSession cancels the current operation on a session's engine.
func (c *HubClient) CancelSession(id string) error {
	resp, err := c.call("session.cancel", sessionIDParams{SessionID: id})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf(resp.Error)
	}
	return nil
}

// Status returns the hub daemon status.
func (c *HubClient) Status() (*HubStatus, error) {
	resp, err := c.call("hub.status", nil)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, fmt.Errorf(resp.Error)
	}

	var status HubStatus
	if err := json.Unmarshal(resp.Result, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// Shutdown tells the hub daemon to stop.
func (c *HubClient) Shutdown() error {
	resp, err := c.call("hub.shutdown", nil)
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf(resp.Error)
	}
	return nil
}
