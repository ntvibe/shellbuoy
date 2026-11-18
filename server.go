package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func jsonReply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func isLocalHost(host string, port int) bool {
	h, p, e := net.SplitHostPort(host)
	return e == nil && p == fmt.Sprint(port) && (h == "127.0.0.1" || h == "localhost")
}
func (a *App) localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocalHost(r.Host, a.uiPort) {
			http.Error(w, "invalid host", 403)
			return
		}
		if r.Method == "POST" {
			origin := r.Header.Get("Origin")
			if origin != "" && origin != fmt.Sprintf("http://127.0.0.1:%d", a.uiPort) && origin != fmt.Sprintf("http://localhost:%d", a.uiPort) {
				http.Error(w, "invalid origin", 403)
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-ShellBuoy-CSRF")), []byte(a.csrf)) != 1 {
				http.Error(w, "invalid CSRF token", 403)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func decodeBody(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 16385)).Decode(v)
}
func (a *App) localMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		b, err := assets.ReadFile("web/index.html")
		if err != nil {
			http.Error(w, "UI unavailable", 500)
			return
		}
		page := strings.ReplaceAll(string(b), "__CSRF_TOKEN__", a.csrf)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		_, _ = io.WriteString(w, page)
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "method", 405)
			return
		}
		a.mu.Lock()
		workspace := a.config.Workspace
		running := a.running
		a.mu.Unlock()
		jsonReply(w, 200, map[string]any{"Workspace": workspace, "Running": running, "History": a.snapshot(), "UIPort": a.uiPort, "MCPPort": a.mcpPort})
	})
	mux.HandleFunc("/api/tailscale", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "method", 405)
			return
		}
		jsonReply(w, 200, tailscaleState(a.mcpPort))
	})
	mux.HandleFunc("/api/workspace", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		var input struct{ Path string }
		if err := decodeBody(r, &input); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		location, err := filepath.Abs(strings.TrimSpace(input.Path))
		if err != nil {
			http.Error(w, "invalid path", 400)
			return
		}
		location, err = filepath.EvalSymlinks(location)
		if err != nil {
			http.Error(w, "directory does not exist", 400)
			return
		}
		stat, err := os.Stat(location)
		if err != nil || !stat.IsDir() {
			http.Error(w, "not a directory", 400)
			return
		}
		a.mu.Lock()
		if a.running != "" {
			a.mu.Unlock()
			http.Error(w, "command is running", 409)
			return
		}
		old := a.config.Workspace
		a.config.Workspace = location
		err = a.saveConfig()
		if err != nil {
			a.config.Workspace = old
		}
		a.mu.Unlock()
		if err != nil {
			http.Error(w, "unable to persist workspace", 500)
			return
		}
		jsonReply(w, 200, map[string]any{"Workspace": location})
	})
	mux.HandleFunc("/api/permissions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		a.mu.Lock()
		workspace := a.config.Workspace
		a.mu.Unlock()
		if workspace == "" {
			jsonReply(w, 200, map[string]any{"Readable": false, "Writable": false, "Reason": "Set workspace first"})
			return
		}
		_, readErr := os.ReadDir(workspace)
		writable := false
		if file, err := os.CreateTemp(workspace, ".shellbuoy-check-*"); err == nil {
			path := file.Name()
			_ = file.Close()
			_ = os.Remove(path)
			writable = true
		}
		jsonReply(w, 200, map[string]any{"Readable": readErr == nil, "Writable": writable})
	})
	mux.HandleFunc("/api/decision", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		var input struct {
			ID      string
			Approve bool
		}
		if err := decodeBody(r, &input); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		if err := a.decide(input.ID, input.Approve); err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		jsonReply(w, 200, map[string]bool{"OK": true})
	})
	mux.HandleFunc("/api/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		a.mu.Lock()
		token := a.config.Token
		a.mu.Unlock()
		jsonReply(w, 200, map[string]string{"Token": token})
	})
	mux.HandleFunc("/api/rotate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		a.mu.Lock()
		old := a.config.Token
		a.config.Token = randomToken(32)
		err := a.saveConfig()
		if err != nil {
			a.config.Token = old
		}
		a.mu.Unlock()
		if err != nil {
			http.Error(w, "unable to save token", 500)
			return
		}
		jsonReply(w, 200, map[string]bool{"OK": true})
	})
	mux.HandleFunc("/api/test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method", 405)
			return
		}
		var input struct {
			Command string
			Args    []string
		}
		if err := decodeBody(r, &input); err != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		result, err := a.enqueue(input.Command, input.Args)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		jsonReply(w, 200, result)
	})
	return a.localOnly(mux)
}

type RPC struct {
	JSONRPC string
	ID      json.RawMessage
	Method  string
	Params  json.RawMessage
}

func rpcResponse(id json.RawMessage, body any) map[string]any {
	var v any
	if len(id) != 0 {
		_ = json.Unmarshal(id, &v)
	}
	return map[string]any{"jsonrpc": "2.0", "id": v, "result": body}
}
func rpcError(id json.RawMessage, code int, msg string) map[string]any {
	var v any
	if len(id) != 0 {
		_ = json.Unmarshal(id, &v)
	}
	return map[string]any{"jsonrpc": "2.0", "id": v, "error": map[string]any{"code": code, "message": msg}}
}
func mcpResult(value any) map[string]any {
	data, _ := json.Marshal(value)
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": string(data)}}, "structuredContent": value}
}
func toolList() []any {
	return []any{
		map[string]any{
			"name":        "exec_command",
			"description": "Queue one executable and explicit argument list in the user's workspace. ALWAYS requires local human approval. Returns a request ID. Does not invoke a shell implicitly.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "Executable (e.g. git, pwd, python); provide arguments separately"},
				"args":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			}, "required": []string{"command"}, "additionalProperties": false},
		},
		map[string]any{
			"name": "command_status", "description": "Read status, exit code and captured output for a queued command.",
			"annotations": map[string]any{"readOnlyHint": true},
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}, "required": []string{"id"}, "additionalProperties": false},
		},
	}
}
func (a *App) mcpMux() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		if r.Method != "POST" {
			http.Error(w, "POST required", 405)
			return
		}
		a.mu.Lock()
		token := a.config.Token
		a.mu.Unlock()
		given := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if len(given) != len(token) || subtle.ConstantTimeCompare([]byte(given), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			jsonReply(w, 401, map[string]string{"error": "Unauthorized"})
			return
		}
		var req RPC
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonReply(w, 400, rpcError(nil, -32700, "Parse error"))
			return
		}
		if req.JSONRPC != "2.0" || req.Method == "" {
			jsonReply(w, 400, rpcError(req.ID, -32600, "Invalid request"))
			return
		}
		if strings.HasPrefix(req.Method, "notifications/") {
			w.WriteHeader(202)
			return
		}
		switch req.Method {
		case "initialize":
			jsonReply(w, 200, rpcResponse(req.ID, map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]any{"name": "ShellBuoy", "version": "0.1.0"}}))
		case "ping":
			jsonReply(w, 200, rpcResponse(req.ID, map[string]any{}))
		case "tools/list":
			jsonReply(w, 200, rpcResponse(req.ID, map[string]any{"tools": toolList()}))
		case "tools/call":
			var params struct {
				Name      string
				Arguments map[string]json.RawMessage
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				jsonReply(w, 200, rpcError(req.ID, -32602, "Invalid parameters"))
				return
			}
			if params.Name == "exec_command" {
				var command string
				var args []string
				_ = json.Unmarshal(params.Arguments["command"], &command)
				if raw, ok := params.Arguments["args"]; ok && json.Unmarshal(raw, &args) != nil {
					jsonReply(w, 200, rpcError(req.ID, -32602, "Invalid args"))
					return
				}
				if len(params.Arguments) > 2 {
					jsonReply(w, 200, rpcError(req.ID, -32602, "Unexpected arguments"))
					return
				}
				rec, err := a.enqueue(command, args)
				if err != nil {
					jsonReply(w, 200, rpcResponse(req.ID, map[string]any{"isError": true, "content": []any{map[string]string{"type": "text", "text": err.Error()}}}))
					return
				}
				jsonReply(w, 200, rpcResponse(req.ID, mcpResult(map[string]any{"id": rec.ID, "status": rec.Status, "message": "Awaiting local human approval. Poll command_status for the outcome."})))
			} else if params.Name == "command_status" {
				var id string
				_ = json.Unmarshal(params.Arguments["id"], &id)
				a.mu.Lock()
				found := a.getRecord(id)
				var result any
				if found != nil {
					result = *found
				} else {
					result = map[string]string{"error": "not found"}
				}
				a.mu.Unlock()
				jsonReply(w, 200, rpcResponse(req.ID, mcpResult(result)))
			} else {
				jsonReply(w, 200, rpcError(req.ID, -32601, "Unknown tool"))
			}
		default:
			jsonReply(w, 200, rpcError(req.ID, -32601, "Unknown method"))
		}
	})
}
