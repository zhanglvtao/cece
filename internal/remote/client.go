package remote

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"cece/internal/ipc"
	"cece/internal/protocol"
)

type CommandFunc func(context.Context, string, ...string) *exec.Cmd

type Options struct {
	BinPath    string
	ProjectDir string
	Command    CommandFunc
}

type Client struct {
	stdin  io.WriteCloser
	events chan protocol.Event
	wait   func() error
	mu     sync.Mutex
}

func New(ctx context.Context, opts Options) (*Client, error) {
	bin := opts.BinPath
	if bin == "" {
		var err error
		bin, err = os.Executable()
		if err != nil {
			return nil, err
		}
	}
	command := opts.Command
	if command == nil {
		command = exec.CommandContext
	}
	cmd := command(ctx, bin, "engine", "--stdio", "--project-dir", opts.ProjectDir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return NewWithPipes(ctx, stdin, stdout, cmd.Wait), nil
}

func NewWithPipes(ctx context.Context, stdin io.WriteCloser, stdout io.Reader, wait func() error) *Client {
	c := &Client{stdin: stdin, events: make(chan protocol.Event, 4096), wait: wait}
	go c.readLoop(ctx, stdout)
	return c
}

func (c *Client) Input(ctx context.Context, input string) error {
	return c.write(ctx, protocol.InputAction{Text: input})
}

func (c *Client) Do(action protocol.Action) {
	_ = c.write(context.Background(), action)
}

func (c *Client) Events() <-chan protocol.Event { return c.events }

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stdin.Close()
}

func (c *Client) write(ctx context.Context, action protocol.Action) error {
	line, err := ipc.MarshalAction(action)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	_, err = c.stdin.Write(append(line, '\n'))
	return err
}

func (c *Client) readLoop(ctx context.Context, stdout io.Reader) {
	defer close(c.events)
	scanner := bufio.NewScanner(stdout)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 8*1024*1024)
	for scanner.Scan() {
		msg, err := ipc.UnmarshalServerMessage(scanner.Bytes())
		if err != nil {
			c.emit(ctx, protocol.RunFailed{Err: err.Error()})
			continue
		}
		if msg.Type == "error" {
			c.emit(ctx, protocol.RunFailed{Err: msg.Message})
			continue
		}
		c.emit(ctx, msg.Event)
	}
	if err := scanner.Err(); err != nil {
		c.emit(ctx, protocol.RunFailed{Err: fmt.Sprintf("engine stdout: %v", err)})
	}
	if c.wait != nil {
		if err := c.wait(); err != nil {
			c.emit(ctx, protocol.RunFailed{Err: err.Error()})
		}
	}
}

func (c *Client) emit(ctx context.Context, ev protocol.Event) {
	select {
	case <-ctx.Done():
	case c.events <- ev:
	}
}
