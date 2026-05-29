package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/PuerkitoBio/goquery"
)

const (
	maxFetchSize = 100 * 1024 // 100KB
	fetchTimeout = 30 * time.Second
)

type webFetchParams struct {
	URL    string `json:"url"`
	Format string `json:"format,omitempty"` // markdown (default), text, html
}

type webFetchTool struct{}

func NewWebFetch() Tool { return webFetchTool{} }

func (webFetchTool) Effect() Effect { return EffectRead }

func (webFetchTool) Info() Definition {
	return Definition{
		Name:        "WebFetch",
		Description: "Fetch a URL and return its content as markdown, text, or HTML. Useful for reading documentation, API references, or any web page.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The URL to fetch content from",
				},
				"format": map[string]any{
					"type":        "string",
					"description": "Output format: markdown (default), text, or html",
					"default":     "markdown",
				},
			},
			"required": []string{"url"},
		},
	}
}

func (webFetchTool) Run(ctx context.Context, input json.RawMessage, emitter Emitter) Result {
	var p webFetchParams
	if err := json.Unmarshal(input, &p); err != nil {
		return Result{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}
	}
	if p.URL == "" {
		return Result{Content: "missing url", IsError: true}
	}
	if !strings.HasPrefix(p.URL, "http://") && !strings.HasPrefix(p.URL, "https://") {
		return Result{Content: "url must start with http:// or https://", IsError: true}
	}

	format := strings.ToLower(p.Format)
	if format == "" {
		format = "markdown"
	}
	if format != "markdown" && format != "text" && format != "html" {
		return Result{Content: "format must be one of: markdown, text, html", IsError: true}
	}

	if emitter != nil {
		emitter.Emit(fmt.Sprintf("Fetching %s", p.URL))
	}

	client := &http.Client{Timeout: fetchTimeout}
	req, err := http.NewRequestWithContext(ctx, "GET", p.URL, nil)
	if err != nil {
		return Result{Content: fmt.Sprintf("create request: %v", err), IsError: true}
	}
	req.Header.Set("User-Agent", "Cece/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return Result{Content: fmt.Sprintf("fetch: %v", err), IsError: true}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{Content: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Status), IsError: true}
	}

	// Read up to maxFetchSize + 1 byte to detect truncation
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchSize+1))
	if err != nil {
		return Result{Content: fmt.Sprintf("read body: %v", err), IsError: true}
	}

	truncated := len(body) > maxFetchSize
	if truncated {
		body = body[:maxFetchSize]
	}

	if !utf8.Valid(body) {
		return Result{Content: "response body is not valid UTF-8", IsError: true}
	}

	content := string(body)
	contentType := resp.Header.Get("Content-Type")
	isHTML := strings.Contains(contentType, "text/html")

	switch format {
	case "markdown":
		if isHTML {
			markdown, err := htmlToMarkdown(content)
			if err != nil {
				return Result{Content: fmt.Sprintf("convert to markdown: %v", err), IsError: true}
			}
			content = markdown
		}
	case "text":
		if isHTML {
			text, err := extractText(content)
			if err != nil {
				return Result{Content: fmt.Sprintf("extract text: %v", err), IsError: true}
			}
			content = text
		}
	case "html":
		if isHTML {
			bodyHTML, err := extractBodyHTML(content)
			if err != nil {
				return Result{Content: fmt.Sprintf("extract body: %v", err), IsError: true}
			}
			content = bodyHTML
		}
	}

	if truncated {
		content += fmt.Sprintf("\n\n[Content truncated at %d bytes]", maxFetchSize)
	}

	return Result{Content: content}
}

func htmlToMarkdown(html string) (string, error) {
	converter := md.NewConverter("", true, nil)
	return converter.ConvertString(html)
}

func extractText(html string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", err
	}
	text := doc.Find("body").Text()
	return strings.TrimSpace(text), nil
}

func extractBodyHTML(html string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", err
	}
	bodyHTML, err := doc.Find("body").Html()
	if err != nil {
		return "", err
	}
	if bodyHTML == "" {
		return "", fmt.Errorf("no body content found")
	}
	return bodyHTML, nil
}
