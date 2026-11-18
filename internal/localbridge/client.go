package localbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Пути HTTP-API моста на сервере agctl dev (контейнер → хост).
const (
	PathTools = "/api/local/bridge/tools"
	PathCall  = "/api/local/bridge/call"
)

// Env-переменные контейнера, через которые клиент находит мост.
const (
	EnvURL   = "AGCTL_BRIDGE_URL"
	EnvToken = "AGCTL_BRIDGE_TOKEN"
)

// CallRequest — тело POST PathCall.
type CallRequest struct {
	Tool string            `json:"tool"`
	Args map[string]string `json:"args"`
}

// Client — клиент моста изнутри контейнера.
type Client struct {
	URL   string
	Token string
	HTTP  *http.Client
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.URL, "/")+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		// подтверждение человека может занять ApprovalTimeout
		hc = &http.Client{Timeout: ApprovalTimeout + DefaultTimeout + time.Minute}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("мост недоступен (%s): %w", c.URL, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = strings.TrimSpace(string(raw))
		}
		return fmt.Errorf("мост: HTTP %d: %s", resp.StatusCode, e.Error)
	}
	return json.Unmarshal(raw, out)
}

// List — инструменты белого списка.
func (c *Client) List(ctx context.Context) ([]Tool, error) {
	var out struct {
		Tools []Tool `json:"tools"`
	}
	if err := c.do(ctx, http.MethodGet, PathTools, nil, &out); err != nil {
		return nil, err
	}
	return out.Tools, nil
}

// Call — вызов инструмента на хосте.
func (c *Client) Call(ctx context.Context, tool string, args map[string]string) (Result, error) {
	var out Result
	err := c.do(ctx, http.MethodPost, PathCall, CallRequest{Tool: tool, Args: args}, &out)
	return out, err
}

// ── MCP (stdio, JSON-RPC 2.0 построчно) ─────────────────────────────────────

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ServeMCP — минимальный MCP-сервер поверх stdio: initialize, tools/list,
// tools/call, ping. Каждый инструмент моста — MCP-инструмент со строковыми
// аргументами.
func ServeMCP(ctx context.Context, in io.Reader, out io.Writer, c *Client) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	enc := json.NewEncoder(out)
	reply := func(id json.RawMessage, result any, rerr *rpcError) error {
		return enc.Encode(rpcMsg{JSONRPC: "2.0", ID: id, Result: result, Error: rerr})
	}
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var msg rpcMsg
		if err := json.Unmarshal(line, &msg); err != nil {
			_ = reply(json.RawMessage("null"), nil, &rpcError{Code: -32700, Message: "parse error"})
			continue
		}
		if len(msg.ID) == 0 { // уведомление (notifications/initialized и пр.)
			continue
		}
		switch msg.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			if p.ProtocolVersion == "" {
				p.ProtocolVersion = "2025-06-18"
			}
			_ = reply(msg.ID, map[string]any{
				"protocolVersion": p.ProtocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": "agctl-host-bridge", "version": "1"},
			}, nil)
		case "ping":
			_ = reply(msg.ID, map[string]any{}, nil)
		case "tools/list":
			tools, err := c.List(ctx)
			if err != nil {
				_ = reply(msg.ID, nil, &rpcError{Code: -32603, Message: err.Error()})
				continue
			}
			_ = reply(msg.ID, map[string]any{"tools": mcpTools(tools)}, nil)
		case "tools/call":
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(msg.Params, &p); err != nil {
				_ = reply(msg.ID, nil, &rpcError{Code: -32602, Message: "invalid params"})
				continue
			}
			args := map[string]string{}
			for k, v := range p.Arguments {
				args[k] = fmt.Sprint(v)
			}
			res, err := c.Call(ctx, p.Name, args)
			if err != nil {
				_ = reply(msg.ID, map[string]any{
					"content": []any{map[string]any{"type": "text", "text": err.Error()}},
					"isError": true,
				}, nil)
				continue
			}
			text, _ := json.MarshalIndent(res, "", "  ")
			_ = reply(msg.ID, map[string]any{
				"content": []any{map[string]any{"type": "text", "text": string(text)}},
				"isError": res.ExitCode != 0,
			}, nil)
		default:
			_ = reply(msg.ID, nil, &rpcError{Code: -32601, Message: "method not found: " + msg.Method})
		}
	}
	return sc.Err()
}

func mcpTools(tools []Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		props := map[string]any{}
		required := []string{}
		names := make([]string, 0, len(t.Args))
		for n := range t.Args {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			a := t.Args[n]
			props[n] = map[string]any{"type": "string", "description": a.Description}
			if a.Required {
				required = append(required, n)
			}
		}
		desc := t.Description
		if t.Confirm {
			desc += " (вызов ждёт подтверждения человека на хосте)"
		}
		out = append(out, map[string]any{
			"name": t.Name, "description": desc,
			"inputSchema": map[string]any{"type": "object", "properties": props, "required": required},
		})
	}
	return out
}
