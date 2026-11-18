package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	a, err := newApp(t.TempDir(), 3997, 3998)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func TestCredentialsAndHistory(t *testing.T) {
	a := newTestApp(t)
	if len(a.config.Token) != 64 {
		t.Fatalf("unexpected token size %d", len(a.config.Token))
	}
	if _, err := a.enqueue("git", []string{"--version"}); err == nil {
		t.Fatal("should require workspace")
	}
	a.config.Workspace = t.TempDir()
	record, err := a.enqueue("git", []string{"--version"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.decide(record.ID, false); err != nil {
		t.Fatal(err)
	}
	b, err := newApp(a.directory, 3997, 3998)
	if err != nil {
		t.Fatal(err)
	}
	if b.config.Token != a.config.Token {
		t.Fatal("token not durable")
	}
	if len(b.records) != 1 || b.records[0].Status != "denied" {
		t.Fatal("history not restored")
	}
}
func TestApprovalRunsCommand(t *testing.T) {
	a := newTestApp(t)
	a.config.Workspace = t.TempDir()
	record, err := a.enqueue("git", []string{"--version"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.decide(record.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := a.decide(record.ID, true); err == nil {
		t.Fatal("duplicate approval")
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		current := a.getRecord(record.ID)
		status := current.Status
		out := current.Output
		a.mu.Unlock()
		if status == "completed" {
			if !strings.Contains(out, "git version") {
				t.Errorf("unexpected output: %q", out)
			}
			return
		}
		if status == "failed" {
			t.Fatalf("execution failed: %s", out)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("command not completed")
}
func TestMCPUnauthorizedAndAllowed(t *testing.T) {
	a := newTestApp(t)
	h := a.mcpMux()
	payload := []byte("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}")
	for _, tc := range []struct {
		token string
		want  int
	}{
		{"", 401}, {"wrong", 401}, {a.config.Token, 200},
	} {
		req := httptest.NewRequest("POST", "http://127.0.0.1:3998/mcp", bytes.NewReader(payload))
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("token %q: got %d want %d", tc.token, w.Code, tc.want)
		}
	}
}
func TestMCPQueueAndRead(t *testing.T) {
	a := newTestApp(t)
	a.config.Workspace = t.TempDir()
	h := a.mcpMux()
	body := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"exec_command\",\"arguments\":{\"command\":\"git\",\"args\":[\"--version\"]}}}"
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+a.config.Token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body)
	}
	if len(a.records) != 1 || a.records[0].Status != "pending" {
		t.Fatal("unapproved command executed")
	}
	if a.running != "" {
		t.Fatal("command started before approval")
	}
}
func TestUIProtection(t *testing.T) {
	a := newTestApp(t)
	h := a.localMux()
	req := httptest.NewRequest("POST", "http://127.0.0.1:3997/api/workspace", strings.NewReader("{}"))
	req.Host = "evil.example:3997"
	req.Header.Set("X-ShellBuoy-CSRF", a.csrf)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("unexpected host accepted: %d", w.Code)
	}
	req = httptest.NewRequest("POST", "http://127.0.0.1:3997/api/workspace", strings.NewReader("{}"))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("missing CSRF accepted: %d", w.Code)
	}
}
func TestWorkspaceUpdateAndPersistence(t *testing.T) {
	a := newTestApp(t)
	path := t.TempDir()
	b, _ := json.Marshal(map[string]string{"Path": path})
	req := httptest.NewRequest("POST", "http://127.0.0.1:3997/api/workspace", bytes.NewReader(b))
	req.Header.Set("X-ShellBuoy-CSRF", a.csrf)
	w := httptest.NewRecorder()
	a.localMux().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("workspace rejected: %d %s", w.Code, w.Body.String())
	}
	configBytes, err := os.ReadFile(filepath.Join(a.directory, "config.json"))
	if err != nil || !bytes.Contains(configBytes, []byte(path)) {
		t.Fatal("workspace not saved")
	}
}
