package setup

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/zhanglvtao/cece/internal/codebase"
)

func TestFetchModelsCodebaseReadsCocoPlugins(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `models:
  - name: openrouter-2o
    context_window: 936000
    byted_trae:
      base_url: https://codebase-api.byted.org/v2/api/2022-06-01/LLMProxy/TraeV2/chat/completions
      config_name: openrouter-2o
      model: openrouter-2o__dev
`
	if err := os.WriteFile(filepath.Join(pluginDir, "coco.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(codebase.CocoPluginsDirEnv, dir)

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer server.Close()

	models, err := fetchModels("codebase", server.URL, "")
	if err != nil {
		t.Fatalf("fetchModels: %v", err)
	}
	if called {
		t.Fatal("codebase fetchModels should not call /v1/models")
	}
	if len(models) != 1 {
		t.Fatalf("models len = %d", len(models))
	}
	if models[0].id != "openrouter-2o__dev" || models[0].configName != "openrouter-2o" {
		t.Fatalf("model = %+v", models[0])
	}
}
