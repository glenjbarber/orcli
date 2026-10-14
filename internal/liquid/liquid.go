// Package liquid implements the Liquid Console OpenAI-compatible inference API.
package liquid

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/glenjbarber/orcli/internal/openrouter"
)

const baseURL = "https://api.liquid.ai/openai/v1"

var ErrNoAPIKey = errors.New("liquid: no API key configured")
var ErrUsageUnsupported = errors.New("liquid: key usage reporting is not documented")

// Client is a Liquid Console inference client. Its endpoint is fixed in production.
type Client struct {
	apiKey string
	base   *url.URL
	http   *http.Client
}

// New creates a client for Liquid Console.
func New(apiKey string) *Client {
	u, _ := url.Parse(baseURL)
	return newClient(apiKey, u, &http.Client{Timeout: 2 * time.Minute})
}

func newClient(apiKey string, base *url.URL, hc *http.Client) *Client {
	copyClient := *hc
	prior := copyClient.CheckRedirect
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != base.Scheme || !strings.EqualFold(req.URL.Host, base.Host) {
			return http.ErrUseLastResponse
		}
		if prior != nil {
			return prior(req, via)
		}
		if len(via) >= 10 {
			return errors.New("liquid: too many redirects")
		}
		return nil
	}
	return &Client{apiKey: apiKey, base: base, http: &copyClient}
}

type wireRequest struct {
	Model    string        `json:"model"`
	Messages []wireMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Tools    []wireTool    `json:"tools,omitempty"`
}
type wireMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []wireCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}
type wireCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function wireFunction `json:"function"`
}
type wireFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type wireTool struct {
	Type     string                  `json:"type"`
	Function openrouter.ToolFunction `json:"function"`
}

func translate(req openrouter.Request) wireRequest {
	w := wireRequest{Model: req.Model, Stream: req.Stream, Tools: make([]wireTool, 0, len(req.Tools))}
	for _, t := range req.Tools {
		w.Tools = append(w.Tools, wireTool{Type: t.Type, Function: t.Function})
	}
	w.Messages = make([]wireMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		wm := wireMessage{Role: m.Role, Content: m.Content, Name: m.Name, ToolCallID: m.ToolCallID}
		for _, tc := range m.ToolCalls {
			wm.ToolCalls = append(wm.ToolCalls, wireCall{ID: tc.ID, Type: tc.Type, Function: wireFunction{Name: tc.Function.Name, Arguments: tc.Function.Arguments}})
		}
		w.Messages = append(w.Messages, wm)
	}
	return w
}

// Chat streams a reply. Liquid documents OpenAI-compatible SSE chat completions.
func (c *Client) Chat(ctx context.Context, req openrouter.Request, onEvent func(openrouter.Event)) error {
	if onEvent == nil {
		return errors.New("liquid: Chat requires an event callback")
	}
	if c.apiKey == "" {
		return ErrNoAPIKey
	}
	body, err := json.Marshal(translate(req))
	if err != nil {
		return err
	}
	u := *c.base
	u.Path = strings.TrimRight(c.base.Path, "/") + "/chat/completions"
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	hreq.Header.Set("Authorization", "Bearer "+c.apiKey)
	hreq.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(hreq)
	if err != nil {
		onEvent(openrouter.Event{Kind: openrouter.EventError, Err: err})
		onEvent(openrouter.Event{Kind: openrouter.EventFinish, Finished: false})
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		onEvent(openrouter.Event{Kind: openrouter.EventError, Err: c.httpError(resp)})
		onEvent(openrouter.Event{Kind: openrouter.EventFinish, Finished: false})
		return nil
	}
	return c.stream(ctx, resp.Body, onEvent)
}

func (c *Client) httpError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	msg := strings.ReplaceAll(strings.TrimSpace(string(b)), c.apiKey, "[redacted]")
	if msg == "" {
		return fmt.Errorf("liquid: HTTP %s", resp.Status)
	}
	return fmt.Errorf("liquid: HTTP %s: %s", resp.Status, msg)
}

type streamChunk struct {
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *openrouter.Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) stream(ctx context.Context, r io.Reader, emit func(openrouter.Event)) error {
	var calls = map[int]*openrouter.ToolCall{}
	finished, reason := false, ""
	var parseErr error
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 2*1024*1024)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			parseErr = err
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			finished = true
			emit(openrouter.Event{Kind: openrouter.EventDone})
			break
		}
		var ch streamChunk
		if err := json.Unmarshal([]byte(data), &ch); err != nil {
			parseErr = fmt.Errorf("liquid: malformed stream chunk")
			break
		}
		if ch.Error != nil {
			message := strings.ReplaceAll(ch.Error.Message, c.apiKey, "[redacted]")
			if len(message) > 512 {
				message = message[:512]
			}
			parseErr = errors.New("liquid: " + message)
			break
		}
		if ch.Usage != nil {
			emit(openrouter.Event{Kind: openrouter.EventUsage, Usage: ch.Usage})
		}
		for _, choice := range ch.Choices {
			if choice.Delta.Content != "" {
				emit(openrouter.Event{Kind: openrouter.EventDelta, Text: choice.Delta.Content})
			}
			for _, frag := range choice.Delta.ToolCalls {
				call := calls[frag.Index]
				if call == nil {
					call = &openrouter.ToolCall{Index: frag.Index}
					calls[frag.Index] = call
				}
				if frag.ID != "" {
					call.ID = frag.ID
				}
				if frag.Type != "" {
					call.Type = frag.Type
				}
				call.Function.Name += frag.Function.Name
				call.Function.Arguments += frag.Function.Arguments
			}
			if choice.FinishReason != "" {
				reason = choice.FinishReason
			}
		}
	}
	if parseErr == nil {
		parseErr = scanner.Err()
	}
	if parseErr == nil && !finished {
		parseErr = errors.New("liquid: stream ended before [DONE]")
	}
	if finished {
		indices := make([]int, 0, len(calls))
		for index := range calls {
			indices = append(indices, index)
		}
		sort.Ints(indices)
		for _, index := range indices {
			if call := calls[index]; call != nil {
				emit(openrouter.Event{Kind: openrouter.EventTool, ToolCall: call})
			}
		}
	}
	if parseErr != nil {
		emit(openrouter.Event{Kind: openrouter.EventError, Err: parseErr})
	}
	emit(openrouter.Event{Kind: openrouter.EventFinish, Finished: finished && parseErr == nil, Reason: reason})
	return nil
}

// ModelInfo is the Liquid model catalog record. The API currently documents IDs
// only; pricing and context limits therefore remain unknown.
type ModelInfo struct {
	ID             string `json:"id"`
	Object         string `json:"object"`
	OwnedBy        string `json:"owned_by"`
	PricingUnknown bool
}

// Models returns the models available to the configured organization.
func (c *Client) Models(ctx context.Context) ([]ModelInfo, error) {
	if c.apiKey == "" {
		return nil, ErrNoAPIKey
	}
	u := *c.base
	u.Path = strings.TrimRight(c.base.Path, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.httpError(resp)
	}
	var result struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4*1024*1024)).Decode(&result); err != nil {
		return nil, fmt.Errorf("liquid: decode models: %w", err)
	}
	for i := range result.Data {
		result.Data[i].PricingUnknown = true
	}
	return result.Data, nil
}

// KeyUsage is unavailable because the documented API has no key-scoped usage endpoint.
func (c *Client) KeyUsage(context.Context) error { return ErrUsageUnsupported }
