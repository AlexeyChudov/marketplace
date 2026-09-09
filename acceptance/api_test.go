// Black-box acceptance tests for marketplace v2 (HTTP REST + JWT + RBAC).
package acceptance_test

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestAcc01_Healthz(t *testing.T) {
	s := startServer(t)
	r := s.do(t, "GET", "/healthz", "", nil)
	if r.status != 200 {
		t.Fatalf("expected 200, got %d", r.status)
	}
}

func TestAcc02_RegisterLoginRefresh(t *testing.T) {
	s := startServer(t)

	// register
	u := s.registerUser(t, "buyer")
	if u.Access == u.Refresh {
		t.Errorf("access and refresh tokens must differ")
	}

	// login with same creds
	r := s.do(t, "POST", "/auth/login", "", map[string]any{
		"email":    u.Email,
		"password": "hunter22",
	})
	if r.status != 200 {
		t.Fatalf("login: expected 200, got %d. body: %s", r.status, r.body)
	}
	var tokens struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	r.decode(t, &tokens)
	if tokens.Access == "" || tokens.Refresh == "" {
		t.Errorf("login: missing tokens. body: %s", r.body)
	}

	// refresh
	r = s.do(t, "POST", "/auth/refresh", "", map[string]any{"refresh_token": u.Refresh})
	if r.status != 200 {
		t.Fatalf("refresh: expected 200, got %d. body: %s", r.status, r.body)
	}
}

func TestAcc03_LoginWrongPassword(t *testing.T) {
	s := startServer(t)
	u := s.registerUser(t, "buyer")
	r := s.do(t, "POST", "/auth/login", "", map[string]any{
		"email":    u.Email,
		"password": "wrong-password",
	})
	if r.status != 401 {
		t.Errorf("expected 401 for wrong password, got %d. body: %s", r.status, r.body)
	}
}

func TestAcc04_RoleGates(t *testing.T) {
	s := startServer(t)
	buyer := s.registerUser(t, "buyer")
	seller := s.registerUser(t, "seller")

	// no token → 401
	r := s.do(t, "POST", "/listings", "", map[string]any{"title": "x", "price": 10.0, "stock": 1})
	if r.status != 401 {
		t.Errorf("POST /listings without token: expected 401, got %d", r.status)
	}

	// buyer token → 403
	r = s.do(t, "POST", "/listings", buyer.Access, map[string]any{"title": "x", "price": 10.0, "stock": 1})
	if r.status != 403 {
		t.Errorf("POST /listings as buyer: expected 403, got %d", r.status)
	}

	// seller token → 201
	r = s.do(t, "POST", "/listings", seller.Access, map[string]any{"title": "x", "price": 10.0, "stock": 1})
	if r.status != 201 {
		t.Errorf("POST /listings as seller: expected 201, got %d. body: %s", r.status, r.body)
	}
}

func TestAcc05_OwnerOnlyEditDelete(t *testing.T) {
	s := startServer(t)
	sellerA := s.registerUser(t, "seller")
	sellerB := s.registerUser(t, "seller")

	id := s.createListing(t, sellerA, "A's item", 10.0, 5)

	// sellerB cannot patch
	r := s.do(t, "PATCH", "/listings/"+id, sellerB.Access, map[string]any{"price": 999.0})
	if r.status != 403 {
		t.Errorf("PATCH foreign listing: expected 403, got %d. body: %s", r.status, r.body)
	}
	r = s.do(t, "DELETE", "/listings/"+id, sellerB.Access, nil)
	if r.status != 403 {
		t.Errorf("DELETE foreign listing: expected 403, got %d", r.status)
	}

	// sellerA can
	r = s.do(t, "PATCH", "/listings/"+id, sellerA.Access, map[string]any{"price": 12.0})
	if r.status != 200 {
		t.Errorf("PATCH own listing: expected 200, got %d. body: %s", r.status, r.body)
	}
	r = s.do(t, "DELETE", "/listings/"+id, sellerA.Access, nil)
	if r.status != 204 {
		t.Errorf("DELETE own listing: expected 204, got %d", r.status)
	}
}

func TestAcc06_BuyHappyPath(t *testing.T) {
	s := startServer(t)
	seller := s.registerUser(t, "seller")
	buyer := s.registerUser(t, "buyer")
	id := s.createListing(t, seller, "Молоко", 50.0, 10)

	r := s.do(t, "POST", "/listings/"+id+"/buy", buyer.Access, map[string]any{"qty": 3})
	if r.status != 200 {
		t.Fatalf("buy: expected 200, got %d. body: %s", r.status, r.body)
	}
	var got struct {
		Remaining int `json:"remaining"`
	}
	r.decode(t, &got)
	if got.Remaining != 7 {
		t.Errorf("remaining: expected 7, got %d. body: %s", got.Remaining, r.body)
	}
}

func TestAcc07_BuyOversell409(t *testing.T) {
	s := startServer(t)
	seller := s.registerUser(t, "seller")
	buyer := s.registerUser(t, "buyer")
	id := s.createListing(t, seller, "Last one", 10.0, 2)

	r := s.do(t, "POST", "/listings/"+id+"/buy", buyer.Access, map[string]any{"qty": 5})
	if r.status != 409 {
		t.Errorf("oversell single: expected 409, got %d. body: %s", r.status, r.body)
	}
}

// TestAcc08_OversellUnderConcurrency is the v2 "feel the race" test:
// 50 concurrent buys of qty=1 against stock=10 must yield exactly 10
// successes and 40 conflicts. If the student's storage isn't protected,
// stock goes negative or 200-count > 10. The -race flag will also blow up.
func TestAcc08_OversellUnderConcurrency(t *testing.T) {
	s := startServer(t)
	seller := s.registerUser(t, "seller")
	buyer := s.registerUser(t, "buyer")
	id := s.createListing(t, seller, "Hot item", 10.0, 10)

	const n = 50
	var ok, conflict, other int64
	var wg sync.WaitGroup
	wg.Add(n)
	start := make(chan struct{})
	for range n {
		go func() {
			defer wg.Done()
			<-start
			r := s.do(t, "POST", "/listings/"+id+"/buy", buyer.Access, map[string]any{"qty": 1})
			switch r.status {
			case 200:
				atomic.AddInt64(&ok, 1)
			case 409:
				atomic.AddInt64(&conflict, 1)
			default:
				atomic.AddInt64(&other, 1)
				t.Logf("unexpected status %d body=%s", r.status, r.body)
			}
		}()
	}
	close(start)
	wg.Wait()

	if other != 0 {
		t.Errorf("got %d responses outside {200,409}", other)
	}
	if ok != 10 || conflict != 40 {
		t.Errorf("expected exactly 10×200 and 40×409, got %d×200 and %d×409 — your store has a race (or no locking)", ok, conflict)
	}

	// Verify final state via GET — stock must be 0.
	r := s.do(t, "GET", "/listings/"+id, "", nil)
	if r.status != 200 {
		t.Fatalf("GET listing: %d", r.status)
	}
	var listing struct {
		Stock int `json:"stock"`
	}
	r.decode(t, &listing)
	if listing.Stock != 0 {
		t.Errorf("final stock: expected 0, got %d", listing.Stock)
	}
}

func TestAcc09_PaginationAndTotalCount(t *testing.T) {
	s := startServer(t)
	seller := s.registerUser(t, "seller")
	const total = 25
	for i := range total {
		s.createListing(t, seller, "item-"+strconv.Itoa(i), 1.0, 1)
	}

	r := s.do(t, "GET", "/listings?limit=5&offset=10", "", nil)
	if r.status != 200 {
		t.Fatalf("expected 200, got %d", r.status)
	}
	var rows []map[string]any
	r.decode(t, &rows)
	if len(rows) != 5 {
		t.Errorf("expected 5 rows, got %d", len(rows))
	}
	tc := r.headers.Get("X-Total-Count")
	if tc != strconv.Itoa(total) {
		t.Errorf("X-Total-Count: expected %d, got %q", total, tc)
	}

	// limit > 100 → 400
	r = s.do(t, "GET", "/listings?limit=999", "", nil)
	if r.status != 400 {
		t.Errorf("limit=999: expected 400, got %d", r.status)
	}
}

func TestAcc10_StructuredValidationError(t *testing.T) {
	s := startServer(t)
	seller := s.registerUser(t, "seller")

	r := s.do(t, "POST", "/listings", seller.Access, map[string]any{
		"title": "",
		"price": -5,
		"stock": -1,
	})
	if r.status != 400 {
		t.Fatalf("expected 400, got %d. body: %s", r.status, r.body)
	}
	var got struct {
		Error  string `json:"error"`
		Fields []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"fields"`
	}
	r.decode(t, &got)
	if got.Error != "validation_failed" {
		t.Errorf("error code: expected 'validation_failed', got %q. body: %s", got.Error, r.body)
	}
	if len(got.Fields) == 0 {
		t.Errorf("expected non-empty fields[]. body: %s", r.body)
	}
	seen := map[string]bool{}
	for _, f := range got.Fields {
		seen[f.Field] = true
	}
	for _, want := range []string{"title", "price", "stock"} {
		if !seen[want] {
			t.Errorf("expected fields[] to include %q. body: %s", want, r.body)
		}
	}
}

func TestAcc11_JWTExpired(t *testing.T) {
	s := startServer(t)
	expired := mintJWT(map[string]any{
		"sub":  "11111111-1111-1111-1111-111111111111",
		"role": "seller",
		"typ":  "access",
		"iat":  time.Now().Add(-2 * time.Hour).Unix(),
		"exp":  time.Now().Add(-1 * time.Hour).Unix(),
	})
	r := s.do(t, "POST", "/listings", expired, map[string]any{"title": "x", "price": 1.0, "stock": 1})
	if r.status != 401 {
		t.Errorf("expired JWT: expected 401, got %d", r.status)
	}
}

func TestAcc12_JWTTampered(t *testing.T) {
	s := startServer(t)
	good := mintJWT(map[string]any{
		"sub":  "11111111-1111-1111-1111-111111111111",
		"role": "seller",
		"typ":  "access",
		"iat":  time.Now().Unix(),
		"exp":  time.Now().Add(time.Hour).Unix(),
	})
	// Flip one char in the signature segment, ensuring the replacement
	// actually differs from the original (avoids a 1/64 flake when the
	// signature already ended in 'A').
	parts := strings.Split(good, ".")
	last := parts[2][len(parts[2])-1]
	repl := byte('A')
	if last == repl {
		repl = 'B'
	}
	parts[2] = parts[2][:len(parts[2])-1] + string(repl)
	tampered := strings.Join(parts, ".")
	r := s.do(t, "POST", "/listings", tampered, map[string]any{"title": "x", "price": 1.0, "stock": 1})
	if r.status != 401 {
		t.Errorf("tampered JWT: expected 401, got %d", r.status)
	}
}

func TestAcc13_RefreshTokenRejectedOnAccessRoute(t *testing.T) {
	s := startServer(t)
	u := s.registerUser(t, "seller")
	// Using the refresh token on a regular protected route must 401.
	r := s.do(t, "POST", "/listings", u.Refresh, map[string]any{"title": "x", "price": 1.0, "stock": 1})
	if r.status != 401 {
		t.Errorf("refresh-as-access: expected 401, got %d", r.status)
	}
}

func TestAcc14_RequestIDHeaderEcho(t *testing.T) {
	s := startServer(t)
	req, _ := http.NewRequest("GET", "http://"+s.addr+"/healthz", nil)
	req.Header.Set("X-Request-ID", "test-rid-12345")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	got := resp.Header.Get("X-Request-ID")
	if got != "test-rid-12345" {
		t.Errorf("X-Request-ID echo: expected 'test-rid-12345', got %q", got)
	}
	// And when client omits it, server must generate one.
	resp2, err := http.Get("http://" + s.addr + "/healthz")
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.Header.Get("X-Request-ID") == "" {
		t.Errorf("server must generate X-Request-ID when client omits it")
	}
}

// TestAcc15_GracefulShutdown: send SIGTERM mid-flight, observe that in-flight
// requests complete cleanly and process exits 0 within the deadline.
func TestAcc15_GracefulShutdown(t *testing.T) {
	s := startServer(t)
	// Don't let the deferred cleanup also shutdown — we'll do it ourselves.
	// (We still want cleanup to run in case of failure; cleanup is no-op if process already gone.)

	seller := s.registerUser(t, "seller")
	buyer := s.registerUser(t, "buyer")
	id := s.createListing(t, seller, "g", 10.0, 100)

	const inflight = 20
	var wg sync.WaitGroup
	var success, fail int64
	wg.Add(inflight)
	for range inflight {
		go func() {
			defer wg.Done()
			r := s.do(t, "POST", "/listings/"+id+"/buy", buyer.Access, map[string]any{"qty": 1})
			if r.status == 200 {
				atomic.AddInt64(&success, 1)
			} else {
				atomic.AddInt64(&fail, 1)
			}
		}()
	}

	// Give requests ~50ms to be on the wire, then send SIGTERM.
	time.Sleep(50 * time.Millisecond)
	if err := s.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}
	wg.Wait()

	if err, ok := s.waitExit(11 * time.Second); !ok {
		_ = s.cmd.Process.Kill()
		t.Fatalf("graceful shutdown took longer than 11s")
	} else if err != nil {
		// exec.Cmd.Wait returns *ExitError if non-zero exit.
		t.Errorf("expected exit code 0 on graceful shutdown, got: %v\nstderr:\n%s", err, s.stderrStr())
	}

	if success == 0 {
		t.Errorf("no in-flight requests completed during shutdown — server probably hard-closed; success=%d fail=%d", success, fail)
	}
}

// TestAcc20_TokenTTLs: access must be short-lived, refresh long-lived.
// Spec says 5 minutes and 7 days; the bounds here are deliberately loose so
// a reasonable choice is not punished. What it does catch is the common
// shortcut of minting both tokens with the same (long) expiry, which quietly
// removes the whole point of having two.
func TestAcc20_TokenTTLs(t *testing.T) {
	s := startServer(t)
	u := s.registerUser(t, "buyer")

	accessTTL := tokenTTL(t, u.Access)
	refreshTTL := tokenTTL(t, u.Refresh)

	if accessTTL < 1*time.Minute || accessTTL > 15*time.Minute {
		t.Errorf("access token TTL is %v — spec asks for ~5 minutes", accessTTL)
	}
	if refreshTTL < 24*time.Hour {
		t.Errorf("refresh token TTL is %v — spec asks for 7 days", refreshTTL)
	}
	if refreshTTL <= accessTTL {
		t.Errorf("refresh TTL (%v) must be longer than access TTL (%v) — otherwise why have two?",
			refreshTTL, accessTTL)
	}
}

// --- Frontend, CORS, deployment surface ---

// TestAcc16_ServesFrontend: GET / returns the embedded single-page console.
// The harness runs the binary from an empty working directory, so a service
// that reads web/index.html off disk fails here — same as it would in a
// container. That is the point: the file must be compiled in via embed.FS.
func TestAcc16_ServesFrontend(t *testing.T) {
	s := startServer(t)

	resp, err := http.Get("http://" + s.addr + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Fatalf("GET /: expected 200, got %d. body: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("GET /: expected Content-Type text/html..., got %q", ct)
	}
	// Markers from web/index.html — if these are missing, something else is
	// being served (or the file was rewritten, which it must not be).
	for _, marker := range []string{"<!doctype html>", `id="listings"`, "Marketplace v2"} {
		if !strings.Contains(string(body), marker) {
			t.Errorf("GET / body does not contain %q — is web/index.html really embedded?", marker)
		}
	}
}

// TestAcc17_UnknownPathStillJSON404 guards against the classic mistake of
// mounting a file server at "/" and letting it swallow every unmatched route.
func TestAcc17_UnknownPathStillJSON404(t *testing.T) {
	s := startServer(t)

	r := s.do(t, "GET", "/definitely-not-a-route", "", nil)
	if r.status != 404 {
		t.Fatalf("unknown path: expected 404, got %d. body: %s", r.status, r.body)
	}
	var got struct {
		Error string `json:"error"`
	}
	r.decode(t, &got)
	if got.Error != "not_found" {
		t.Errorf("unknown path: expected error %q, got %q. body: %s", "not_found", got.Error, r.body)
	}
}

// TestAcc18_CORSPreflight: the browser sends OPTIONS before a cross-origin
// POST with a JSON body. If the preflight is not answered, the real request
// never leaves the browser — and nothing shows up in the server logs, which
// is what makes this bug so confusing the first time.
func TestAcc18_CORSPreflight(t *testing.T) {
	s := startServer(t)

	req, _ := http.NewRequest("OPTIONS", "http://"+s.addr+"/listings", nil)
	req.Header.Set("Origin", "https://marketplace-frontend.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type,authorization")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		t.Fatalf("preflight: expected 2xx, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got == "" {
		t.Errorf("preflight: missing Access-Control-Allow-Origin header")
	}
	allowed := strings.ToUpper(resp.Header.Get("Access-Control-Allow-Methods"))
	if allowed != "" && !strings.Contains(allowed, "POST") {
		t.Errorf("preflight: Access-Control-Allow-Methods %q does not allow POST", allowed)
	}
}

// TestAcc19_PortFromEnv: no --addr flag, PORT set. This is exactly how Render,
// Railway and Fly start the process; a service hardcoded to :8080 dies there.
func TestAcc19_PortFromEnv(t *testing.T) {
	s, port := startServerWithPort(t)

	want := ":" + strconv.Itoa(port)
	if !strings.HasSuffix(s.addr, want) {
		t.Fatalf("PORT=%d: server reported address %q, expected it to end with %q", port, s.addr, want)
	}

	r := s.do(t, "GET", "/healthz", "", nil)
	if r.status != 200 {
		t.Errorf("healthz on PORT-derived address: expected 200, got %d", r.status)
	}
}
