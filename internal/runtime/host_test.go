package runtime

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/zhanglvtao/cece/internal/protocol"
)

func TestHostDelegatesInputAndEventsToForegroundRuntime(t *testing.T) {
	host, err := BuildHost(Options{
		ProjectDir:    t.TempDir(),
		Model:         "test-model",
		ContextWindow: 32000,
		MaxTokens:     1024,
		Yolo:          true,
		ModelClient:   stubModelClient{},
		Store:         newMemStore(),
	})
	if err != nil {
		t.Fatalf("BuildHost error = %v", err)
	}
	t.Cleanup(host.Close)

	if err := host.Input(context.Background(), "hello from host"); err != nil {
		t.Fatalf("Input error = %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-host.Events():
			if _, ok := ev.(protocol.SessionCreated); ok {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for SessionCreated from host events")
		}
	}
}

func TestHostDoDelegatesToMediator(t *testing.T) {
	host, err := BuildHost(Options{
		ProjectDir:    t.TempDir(),
		Model:         "test-model",
		ContextWindow: 32000,
		MaxTokens:     1024,
		Yolo:          true,
		ModelClient:   stubModelClient{},
		Store:         newMemStore(),
		ListAllModelsFn: func(context.Context) ([]protocol.ModelInfo, error) {
			return []protocol.ModelInfo{{ID: "test-model", MaxContextWindow: 32000}}, nil
		},
	})
	if err != nil {
		t.Fatalf("BuildHost error = %v", err)
	}
	t.Cleanup(host.Close)

	host.Do(protocol.ListModelsAction{})

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-host.Events():
			models, ok := ev.(protocol.ModelsLoadedEvent)
			if !ok {
				continue
			}
			if len(models.Models) != 1 || models.Models[0].ID != "test-model" {
				t.Fatalf("ModelsLoadedEvent = %+v, want single test-model", models)
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for ModelsLoadedEvent from host events")
		}
	}
}

func TestHostListModelsFallsBackToCurrentModelWhenListAllModelsIsUnset(t *testing.T) {
	host, err := BuildHost(Options{
		ProjectDir:    t.TempDir(),
		Model:         "test-model",
		ContextWindow: 32000,
		MaxTokens:     1024,
		Yolo:          true,
		ModelClient:   stubModelClient{},
		Store:         newMemStore(),
	})
	if err != nil {
		t.Fatalf("BuildHost error = %v", err)
	}
	t.Cleanup(host.Close)

	host.Do(protocol.ListModelsAction{})

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-host.Events():
			models, ok := ev.(protocol.ModelsLoadedEvent)
			if !ok {
				continue
			}
			if len(models.Models) != 1 || models.Models[0].ID != "test-model" || models.Models[0].MaxContextWindow != 32000 {
				t.Fatalf("ModelsLoadedEvent = %+v, want fallback current model", models)
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for fallback ModelsLoadedEvent from host events")
		}
	}
}

func TestHostLogsLifecycleAndActions(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	orig := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(orig) })

	host, err := BuildHost(Options{
		ProjectDir:    t.TempDir(),
		Model:         "test-model",
		ContextWindow: 32000,
		MaxTokens:     1024,
		Yolo:          true,
		ModelClient:   stubModelClient{},
		Store:         newMemStore(),
	})
	if err != nil {
		t.Fatalf("BuildHost error = %v", err)
	}
	defer host.Close()

	if err := host.Input(context.Background(), "hello from host"); err != nil {
		t.Fatalf("Input error = %v", err)
	}
	host.Do(protocol.ListModelsAction{})
	host.Wait()
	host.Close()

	logs := buf.String()
	checks := []string{
		"runtime host: started",
		"model=test-model",
		"runtime host: input",
		"operation=input",
		"runtime host: action",
		"action=protocol.ListModelsAction",
		"runtime host: shutdown",
	}
	for _, check := range checks {
		if !strings.Contains(logs, check) {
			t.Fatalf("logs missing %q:\n%s", check, logs)
		}
	}
}
