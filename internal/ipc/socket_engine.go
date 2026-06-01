package ipc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
)

// ServeSocket listens on a Unix socket and serves the runtime over each
// connection. When a client disconnects the engine stays alive, waiting
// for the next connection. The loop exits only when ctx is cancelled.
func ServeSocket(ctx context.Context, runtime Runtime, socketPath string) error {
	// Clean up stale socket file.
	os.Remove(socketPath)

	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", socketPath, err)
	}
	defer l.Close()

	// Ensure socket is cleaned up on exit.
	go func() {
		<-ctx.Done()
		l.Close()
		os.Remove(socketPath)
	}()

	slog.Info("engine socket listening", "path", socketPath)

	for {
		conn, err := l.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("accept: %w", err)
			}
		}

		slog.Info("engine socket client connected")

		// Serve this connection. Serve returns when the client
		// disconnects — we then loop back to accept the next one.
		if err := Serve(ctx, runtime, conn, conn); err != nil {
			slog.Warn("engine socket connection ended", "error", err)
		}

		conn.Close()
		slog.Info("engine socket client disconnected, waiting for next connection")
	}
}
