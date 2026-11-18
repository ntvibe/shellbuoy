package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed web/index.html
var assets embed.FS

const maxOutput = 256 * 1024

type Config struct {
	Workspace string
	Token     string
}
type Record struct {
	ID          string
	Command     string
	Args        []string
	Workspace   string
	Status      string
	RequestedAt string
	StartedAt   string
	FinishedAt  string
	Output      string
	Error       string
	ExitCode    int
	Truncated   bool
}
type TailscaleState struct {
	Installed     bool
	Connected     bool
	BackendState  string
	Hostname      string
	URL           string
	FunnelActive  bool
	FunnelDetails string
}
type App struct {
	mu        sync.Mutex
	config    Config
	directory string
	records   []*Record
	running   string
	csrf      string
	uiPort    int
	mcpPort   int
}

func randomToken(size int) string {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func newApp(dir string, uiPort, mcpPort int) (*App, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	a := &App{directory: dir, csrf: randomToken(24), uiPort: uiPort, mcpPort: mcpPort}
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err == nil {
		if err = json.Unmarshal(b, &a.config); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if a.config.Token == "" {
		a.config.Token = randomToken(32)
		if err := a.saveConfig(); err != nil {
			return nil, err
		}
	}
	history, err := os.Open(filepath.Join(dir, "history.jsonl"))
	if err == nil {
		defer history.Close()
		dec := json.NewDecoder(history)
		latest := make(map[string]*Record)
		var order []string
		for {
			var item Record
			if e := dec.Decode(&item); e != nil {
				break
			}
			if _, exists := latest[item.ID]; !exists {
				order = append(order, item.ID)
			}
			cp := item
			latest[item.ID] = &cp
		}
		for _, id := range order {
			r := latest[id]
			if r.Status == "pending" || r.Status == "running" {
				r.Status = "interrupted"
				r.Error = "Application stopped before command completion"
				r.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			}
			a.records = append(a.records, r)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return a, nil
}
func (a *App) saveConfig() error {
	b, err := json.MarshalIndent(a.config, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(a.directory, "config.json")
	temp := target + ".tmp"
	if err := os.WriteFile(temp, b, 0600); err != nil {
		return err
	}
	return os.Rename(temp, target)
}
func (a *App) persist(r *Record) {
	f, err := os.OpenFile(filepath.Join(a.directory, "history.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		log.Printf("history write: %v", err)
		return
	}
	defer f.Close()
	b, _ := json.Marshal(r)
	_, _ = f.Write(append(b, '\n'))
}
func (a *App) snapshot() []Record {
	a.mu.Lock()
	defer a.mu.Unlock()
	start := len(a.records) - 100
	if start < 0 {
		start = 0
	}
	out := make([]Record, 0, len(a.records)-start)
	for i := len(a.records) - 1; i >= start; i-- {
		out = append(out, *a.records[i])
	}
	return out
}
func (a *App) getRecord(id string) *Record {
	for _, r := range a.records {
		if r.ID == id {
			return r
		}
	}
	return nil
}
func (a *App) enqueue(cmd string, args []string) (Record, error) {
	if cmd == "" || strings.ContainsAny(cmd, "\x00\r\n") || len(cmd) > 512 || len(args) > 64 {
		return Record{}, errors.New("invalid executable or argument count")
	}
	total := len(cmd)
	for _, arg := range args {
		total += len(arg)
		if strings.ContainsRune(arg, '\x00') {
			return Record{}, errors.New("NUL not allowed")
		}
	}
	if total > 8192 {
		return Record{}, errors.New("command too large")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.config.Workspace == "" {
		return Record{}, errors.New("select a workspace locally first")
	}
	if len(a.records) >= 2000 {
		return Record{}, errors.New("history limit reached; archive history to continue")
	}
	r := &Record{ID: randomToken(10), Command: cmd, Args: args, Workspace: a.config.Workspace, Status: "pending", RequestedAt: time.Now().UTC().Format(time.RFC3339)}
	a.records = append(a.records, r)
	a.persist(r)
	return *r, nil
}

type writer struct {
	a *App
	r *Record
}

func (w writer) Write(p []byte) (int, error) {
	w.a.mu.Lock()
	defer w.a.mu.Unlock()
	n := len(p)
	if len(w.r.Output) < maxOutput {
		remaining := maxOutput - len(w.r.Output)
		if n > remaining {
			w.r.Output += string(p[:remaining])
			w.r.Truncated = true
		} else {
			w.r.Output += string(p)
		}
	} else {
		w.r.Truncated = true
	}
	w.a.persist(w.r)
	return n, nil
}
func minimalEnv() []string {
	keys := []string{"PATH", "HOME", "USER", "LOGNAME", "LANG", "LC_ALL", "TMPDIR", "TMP", "TEMP", "SystemRoot", "ComSpec", "PATHEXT", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "WINDIR"}
	env := make([]string, 0, len(keys))
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}
func (a *App) execute(r *Record) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.Command, r.Args...)
	cmd.Dir = r.Workspace
	cmd.Env = minimalEnv()
	cmd.Stdout = writer{a, r}
	cmd.Stderr = writer{a, r}
	err := cmd.Run()
	a.mu.Lock()
	defer a.mu.Unlock()
	r.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if err == nil {
		r.Status = "completed"
		r.ExitCode = 0
	} else {
		r.Status = "failed"
		r.Error = err.Error()
		r.ExitCode = -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			r.ExitCode = exit.ExitCode()
		}
		if ctx.Err() == context.DeadlineExceeded {
			r.Error = "command timed out after 120 seconds"
		}
	}
	a.running = ""
	a.persist(r)
}
func (a *App) decide(id string, approve bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.getRecord(id)
	if r == nil {
		return errors.New("command not found")
	}
	if r.Status != "pending" {
		return errors.New("command already decided")
	}
	if !approve {
		r.Status = "denied"
		r.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		a.persist(r)
		return nil
	}
	if a.running != "" {
		return errors.New("another command is running")
	}
	a.running = id
	r.Status = "running"
	r.StartedAt = time.Now().UTC().Format(time.RFC3339)
	a.persist(r)
	go a.execute(r)
	return nil
}
func cmdOutput(ctx context.Context, exe string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, exe, args...).CombinedOutput()
	return string(out), err
}
func tailscaleState(port int) TailscaleState {
	state := TailscaleState{}
	binary, err := exec.LookPath("tailscale")
	if err != nil {
		return state
	}
	state.Installed = true
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw, err := cmdOutput(ctx, binary, "status", "--json")
	if err != nil {
		state.BackendState = "Unavailable"
		return state
	}
	var s struct {
		BackendState string
		Self         struct{ DNSName string }
	}
	if json.Unmarshal([]byte(raw), &s) != nil {
		return state
	}
	state.BackendState = s.BackendState
	state.Connected = s.BackendState == "Running"
	state.Hostname = strings.TrimSuffix(s.Self.DNSName, ".")
	if state.Connected && state.Hostname != "" {
		state.URL = "https://" + state.Hostname + "/mcp"
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	funnel, e := cmdOutput(ctx2, binary, "funnel", "status")
	if e == nil {
		state.FunnelDetails = strings.TrimSpace(funnel)
		state.FunnelActive = strings.Contains(funnel, fmt.Sprintf("127.0.0.1:%d", port)) || strings.Contains(funnel, fmt.Sprintf("localhost:%d", port))
	}
	return state
}
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
func main() {
	uiPort := flag.Int("ui-port", 3997, "local dashboard port")
	mcpPort := flag.Int("mcp-port", 3998, "local MCP port")
	noBrowser := flag.Bool("no-browser", false, "do not open browser")
	flag.Parse()
	if *uiPort == *mcpPort {
		log.Fatal("UI and MCP ports must differ")
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	a, err := newApp(filepath.Join(dir, "shellbuoy"), *uiPort, *mcpPort)
	if err != nil {
		log.Fatal(err)
	}
	mcp, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *mcpPort))
	if err != nil {
		log.Fatal(err)
	}
	ui, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *uiPort))
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		log.Printf("MCP: http://127.0.0.1:%d/mcp (bearer auth)", *mcpPort)
		if e := http.Serve(mcp, a.mcpMux()); e != nil {
			log.Fatal(e)
		}
	}()
	url := fmt.Sprintf("http://127.0.0.1:%d", *uiPort)
	log.Printf("ShellBuoy UI: %s", url)
	log.Printf("Private config/history: %s", a.directory)
	if !*noBrowser {
		go func() { time.Sleep(300 * time.Millisecond); openBrowser(url) }()
	}
	log.Fatal(http.Serve(ui, a.localMux()))
}
