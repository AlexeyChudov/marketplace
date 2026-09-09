// Helpers for the marketplace v2 acceptance harness.
//
// Boots the student's binary, picks a free port, waits for the agreed
// "listening on http://127.0.0.1:PORT" startup line, exposes a small
// HTTP client and a hand-rolled HS256 JWT minter.
package acceptance_test

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const jwtSecret = "test-secret"

// process state shared by all tests in this package.
type server struct {
	cmd  *exec.Cmd
	addr string // host:port

	bufMu  sync.Mutex // guards stdout/stderr
	stdout *bytes.Buffer
	stderr *bytes.Buffer

	done    chan struct{} // closed exactly once when cmd.Wait returns
	exitMu  sync.Mutex
	exitErr error
}

// stdoutStr / stderrStr return a snapshot under the buffer lock so callers
// can read concurrently with the drain goroutines without tripping -race.
func (s *server) stdoutStr() string {
	s.bufMu.Lock()
	defer s.bufMu.Unlock()
	return s.stdout.String()
}

func (s *server) stderrStr() string {
	s.bufMu.Lock()
	defer s.bufMu.Unlock()
	return s.stderr.String()
}

func (s *server) waitExit(d time.Duration) (error, bool) {
	select {
	case <-s.done:
		s.exitMu.Lock()
		defer s.exitMu.Unlock()
		return s.exitErr, true
	case <-time.After(d):
		return nil, false
	}
}

var startupRE = regexp.MustCompile(`listening on (http://127\.0\.0\.1:\d+)`)

// startServer builds the student binary if needed, launches it, blocks until
// it prints the startup line, returns a control handle.
func startServer(t *testing.T) *server {
	t.Helper()
	return launch(t, buildBinary(t), []string{"--addr=:0"}, nil)
}

// startServerWithPort boots the binary with no --addr flag and PORT set,
// which is how every PaaS runs it. Returns the server and the port it was
// told to use.
func startServerWithPort(t *testing.T) (*server, int) {
	t.Helper()

	bin := buildBinary(t)

	// Grab a free port, release it, hand the number to the binary. There is a
	// small race window here, which is why this is the only test using it.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	return launch(t, bin, nil, []string{"PORT=" + strconv.Itoa(port)}), port
}

// buildBinary resolves MARKETPLACE_BIN, or builds MARKETPLACE_SRC into a temp dir.
func buildBinary(t *testing.T) string {
	t.Helper()

	bin := os.Getenv("MARKETPLACE_BIN")
	if bin == "" {
		src := os.Getenv("MARKETPLACE_SRC")
		if src == "" {
			t.Fatalf("set MARKETPLACE_BIN=/path/to/binary or MARKETPLACE_SRC=/path/to/project")
		}
		tmp, err := os.MkdirTemp("", "marketplace-v2-bin-*")
		if err != nil {
			t.Fatalf("mkdir temp: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(tmp) })
		bin = filepath.Join(tmp, "marketplace-api")
		build := exec.Command("go", "build", "-o", bin, ".")
		build.Dir = src
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("go build failed:\n%s\n%v", out, err)
		}
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("binary not found: %s", bin)
	}
	if !filepath.IsAbs(bin) {
		abs, err := filepath.Abs(bin)
		if err != nil {
			t.Fatalf("abs path for %s: %v", bin, err)
		}
		bin = abs
	}
	return bin
}

// launch runs the binary and blocks until it prints the startup line.
//
// The process is started in an empty working directory on purpose: the
// frontend must be compiled into the binary via embed.FS, so a service that
// reads web/index.html off disk at runtime fails here exactly like it would
// fail inside a container.
func launch(t *testing.T, bin string, args, extraEnv []string) *server {
	t.Helper()

	runDir, err := os.MkdirTemp("", "marketplace-v2-run-*")
	if err != nil {
		t.Fatalf("mkdir run dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runDir) })

	cmd := exec.Command(bin, args...)
	cmd.Dir = runDir
	cmd.Env = append(os.Environ(), "JWT_SECRET="+jwtSecret)
	cmd.Env = append(cmd.Env, extraEnv...)
	stdoutPipe, _ := cmd.StdoutPipe()
	stderrPipe, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	srv := &server{cmd: cmd, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, done: make(chan struct{})}

	// Drain stderr and collect stdout, watching for the startup line.
	addrCh := make(chan string, 1)
	var once sync.Once
	go func() {
		sc := bufio.NewScanner(stdoutPipe)
		for sc.Scan() {
			line := sc.Text()
			srv.bufMu.Lock()
			srv.stdout.WriteString(line + "\n")
			srv.bufMu.Unlock()
			if m := startupRE.FindStringSubmatch(line); m != nil {
				once.Do(func() { addrCh <- strings.TrimPrefix(m[1], "http://") })
			}
		}
	}()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderrPipe.Read(buf)
			if n > 0 {
				srv.bufMu.Lock()
				srv.stderr.Write(buf[:n])
				srv.bufMu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		err := cmd.Wait()
		srv.exitMu.Lock()
		srv.exitErr = err
		srv.exitMu.Unlock()
		close(srv.done)
	}()

	select {
	case addr := <-addrCh:
		srv.addr = addr
	case <-srv.done:
		srv.exitMu.Lock()
		err := srv.exitErr
		srv.exitMu.Unlock()
		t.Fatalf("binary exited before startup line: err=%v\nstdout:\n%s\nstderr:\n%s", err, srv.stdoutStr(), srv.stderrStr())
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("binary did not print startup line within 10s\nstdout:\n%s\nstderr:\n%s", srv.stdoutStr(), srv.stderrStr())
	}

	// Probe /healthz so we don't race the listener.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + srv.addr + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				t.Cleanup(func() { srv.shutdown(t) })
				return srv
			}
		}
		// A process that printed the startup line and then died usually
		// panicked while wiring up routes — surface that instead of timing out.
		select {
		case <-srv.done:
			t.Fatalf("process died right after printing the startup line.\n"+
				"Most often: //go:embed failed, or a panic while building the router. "+
				"Note the binary runs from an empty directory, so reading web/index.html "+
				"off disk cannot work — it must be embedded.\nstdout:\n%s\nstderr:\n%s",
				srv.stdoutStr(), srv.stderrStr())
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}
	srv.shutdown(t)
	hint := ""
	for _, e := range extraEnv {
		if strings.HasPrefix(e, "PORT=") {
			hint = fmt.Sprintf("\nHint: %s was set and no --addr flag was passed, "+
				"so the service must listen on that port. It reported %q instead.",
				e, srv.addr)
		}
	}
	t.Fatalf("%s/healthz never returned 200 within 5s%s\nstdout:\n%s\nstderr:\n%s",
		"http://"+srv.addr, hint, srv.stdoutStr(), srv.stderrStr())
	return nil
}

func (s *server) shutdown(t *testing.T) {
	t.Helper()
	if s.cmd.Process == nil {
		return
	}
	// done is closed exactly once, so it's safe to re-check after the test
	// body already consumed the exit (e.g. graceful-shutdown test).
	select {
	case <-s.done:
		return
	default:
	}
	_ = s.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-s.done:
	case <-time.After(12 * time.Second):
		_ = s.cmd.Process.Kill()
		t.Logf("forced kill after 12s\nstdout:\n%s\nstderr:\n%s", s.stdoutStr(), s.stderrStr())
	}
}

// --- HTTP client helpers ---

type response struct {
	status  int
	headers http.Header
	body    []byte
}

func (s *server) do(t *testing.T, method, path, token string, body any) response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "http://"+s.addr+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return response{status: resp.StatusCode, headers: resp.Header, body: raw}
}

func (r response) decode(t *testing.T, into any) {
	t.Helper()
	if err := json.Unmarshal(r.body, into); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, r.body)
	}
}

// --- JWT (HS256) minter, dep-free ---

func mintJWT(claims map[string]any) string {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	hb, _ := json.Marshal(header)
	cb, _ := json.Marshal(claims)
	p1 := base64.RawURLEncoding.EncodeToString(hb)
	p2 := base64.RawURLEncoding.EncodeToString(cb)
	msg := p1 + "." + p2
	mac := hmac.New(sha256.New, []byte(jwtSecret))
	mac.Write([]byte(msg))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return msg + "." + sig
}

// tokenTTL reads exp/iat out of a JWT payload without verifying the
// signature — the payload is plain base64url, which is the point.
func tokenTTL(t *testing.T, token string) time.Duration {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %q", token)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT payload: %v", err)
	}
	var c struct {
		Iat int64 `json:"iat"`
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("parse JWT claims: %v\npayload: %s", err, raw)
	}
	if c.Exp == 0 {
		t.Fatalf("JWT has no exp claim — token never expires. payload: %s", raw)
	}
	if c.Iat == 0 {
		// iat is required by the spec, but fall back to "now" so the TTL
		// assertion still says something useful instead of exploding.
		t.Errorf("JWT has no iat claim (spec 3.4 requires it). payload: %s", raw)
		return time.Until(time.Unix(c.Exp, 0))
	}
	return time.Duration(c.Exp-c.Iat) * time.Second
}

// --- Test fixtures ---

type user struct {
	ID      string
	Email   string
	Role    string
	Access  string
	Refresh string
}

// registerUser hits the real /auth/register endpoint and returns the user
// with tokens. The harness does NOT mint tokens for this — it uses the
// student's own auth surface so we test the whole flow.
func (s *server) registerUser(t *testing.T, role string) *user {
	t.Helper()
	email := fmt.Sprintf("%s-%d@test.local", role, time.Now().UnixNano())
	resp := s.do(t, "POST", "/auth/register", "", map[string]any{
		"email":    email,
		"password": "hunter22",
		"role":     role,
	})
	if resp.status != 201 {
		t.Fatalf("register %s: expected 201, got %d. body: %s", role, resp.status, resp.body)
	}
	var got struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
		User    struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"user"`
	}
	resp.decode(t, &got)
	if got.Access == "" || got.Refresh == "" {
		t.Fatalf("register response missing tokens. body: %s", resp.body)
	}
	return &user{ID: got.User.ID, Email: email, Role: role, Access: got.Access, Refresh: got.Refresh}
}

// createListing helper: returns the new listing's id.
func (s *server) createListing(t *testing.T, seller *user, title string, price float64, stock int) string {
	t.Helper()
	resp := s.do(t, "POST", "/listings", seller.Access, map[string]any{
		"title": title, "price": price, "stock": stock,
	})
	if resp.status != 201 {
		t.Fatalf("create listing: expected 201, got %d. body: %s", resp.status, resp.body)
	}
	var got struct {
		ID string `json:"id"`
	}
	resp.decode(t, &got)
	if got.ID == "" {
		t.Fatalf("create listing: empty id in response. body: %s", resp.body)
	}
	return got.ID
}
