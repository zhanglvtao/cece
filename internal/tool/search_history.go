package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const SearchHistoryToolName = "SearchHistory"

// SearchMatch represents a single match in conversation history.
type SearchMatch struct {
	Turn    int    `json:"turn"`
	Role    string `json:"role"`
	Content string `json:"content"`
}

// SearchHistoryHandler is the callback interface for the SearchHistory tool.
type SearchHistoryHandler struct {
	Search func(query string, maxResults int, isRegex bool) ([]SearchMatch, error)
}

// NewSearchHistory creates a SearchHistory tool that searches conversation history.
func NewSearchHistory(h *SearchHistoryHandler) Tool {
	return searchHistoryTool{handler: h}
}

type searchHistoryTool struct {
	handler *SearchHistoryHandler
}

func (searchHistoryTool) Effect() Effect { return EffectRead }

func (searchHistoryTool) Info() Definition {
	return Definition{
		Name:        SearchHistoryToolName,
		Description: "Search the full conversation history (including compacted/pruned messages) by keyword, substring, or regex pattern. Returns matching messages with turn number, role, and a content snippet. Use this to recall past decisions, code snippets, file paths, error messages, or user requests that may have been compressed or pruned from context.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"query"},
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The search query. Use a substring for simple keyword matching, or a regex pattern when regex=true.",
				},
				"max_results": map[string]any{
					"type":        "integer",
					"description": "Maximum number of results to return. Defaults to 20.",
				},
				"regex": map[string]any{
					"type":        "boolean",
					"description": "Treat the query as a Go regex pattern. Defaults to false (substring match).",
				},
			},
		},
	}
}

type searchHistoryParams struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
	Regex      bool   `json:"regex,omitempty"`
}

func (t searchHistoryTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	if t.handler == nil {
		return Result{Content: "search history handler is not configured", IsError: true}
	}

	var p searchHistoryParams
	if err := json.Unmarshal(input, &p); err != nil {
		return Result{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}
	}

	if strings.TrimSpace(p.Query) == "" {
		return Result{Content: "query is required and must be non-empty", IsError: true}
	}

	if p.Regex {
		if _, err := regexp.Compile(p.Query); err != nil {
			return Result{Content: fmt.Sprintf("invalid regex: %v", err), IsError: true}
		}
	}

	if p.MaxResults <= 0 {
		p.MaxResults = 20
	}

	emitter.Emit(fmt.Sprintf("Searching history for: %q\n", p.Query))

	matches, err := t.handler.Search(p.Query, p.MaxResults, p.Regex)
	if err != nil {
		return Result{Content: fmt.Sprintf("search failed: %v", err), IsError: true}
	}

	if len(matches) == 0 {
		return Result{Content: fmt.Sprintf("No matches found for %q in conversation history.", p.Query)}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d matches for %q:\n\n", len(matches), p.Query))
	for _, m := range matches {
		b.WriteString(fmt.Sprintf("Turn %d (%s): %q\n", m.Turn, m.Role, m.Content))
	}
	return Result{Content: b.String()}
}