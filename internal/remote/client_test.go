package remote

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/zhanglvtao/cece/internal/ipc"
	"github.com/zhanglvtao/cece/internal/protocol"
)

func TestClientWritesInputAndAction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	inW := &writeBuffer{}
	client := NewWithPipes(ctx, inW, strings.NewReader(""), func() error { return nil })

	if err := client.Input(ctx, "hi"); err != nil {
		t.Fatalf("Input: %v", err)
	}
	client.Do(protocol.ConfirmAction{})

	text := inW.String()
	if !strings.Contains(text, `"kind":"input"`) || !strings.Contains(text, `"hi"`) {
		t.Fatalf("input line missing: %q", text)
	}
	if !strings.Contains(text, `"kind":"confirm"`) {
		t.Fatalf("confirm line missing: %q", text)
	}
	_ = client.Close()
}

type writeBuffer struct{ bytes.Buffer }

func (w *writeBuffer) Close() error { return nil }

func TestClientReadsEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, inW := io.Pipe()
	outR, outW := io.Pipe()
	client := NewWithPipes(ctx, inW, outR, func() error { return nil })

	line, _ := ipc.MarshalEvent(protocol.AssistantDelta{Text: "hello"})
	_, _ = outW.Write(append(line, '\n'))

	select {
	case ev := <-client.Events():
		if got, ok := ev.(protocol.AssistantDelta); !ok || got.Text != "hello" {
			t.Fatalf("event = %#v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting event")
	}
	_ = outW.Close()
	_ = client.Close()
}

func TestNewStartsEngineCommand(t *testing.T) {
	if os.Getenv("CECE_REMOTE_HELPER") == "1" {
		select {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	called := false
	client, err := New(ctx, Options{
		BinPath:    os.Args[0],
		ProjectDir: "/tmp/project",
		Command: func(ctx context.Context, bin string, args ...string) *exec.Cmd {
			called = true
			joined := strings.Join(args, " ")
			if bin != os.Args[0] || !strings.Contains(joined, "engine --stdio") || !strings.Contains(joined, "--project-dir /tmp/project") {
				t.Fatalf("bin=%s args=%v", bin, args)
			}
			cmd := exec.CommandContext(ctx, os.Args[0])
			cmd.Env = append(os.Environ(), "CECE_REMOTE_HELPER=1")
			return cmd
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !called || client == nil {
		t.Fatalf("called=%v client=%v", called, client)
	}
	_ = client.Close()
}
