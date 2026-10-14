package gomcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/diegoclair/gomcp"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const validToken = "fcm_valid"

type echoIn struct {
	Text string `json:"text" jsonschema:"the text to echo"`
}

type echoOut struct {
	Text   string `json:"text"`
	UserID string `json:"userId"`
	Scoped string `json:"scoped"`
}

type scopeKey struct{}

var errBusiness = errors.New("card not found")

type recorder struct {
	mu              sync.Mutex
	preCalls        []gomcp.Call
	postErrs        []error
	protocolHeaders []string
}

func (r *recorder) verify(_ context.Context, token string, req *http.Request) (*auth.TokenInfo, error) {
	r.mu.Lock()
	r.protocolHeaders = append(r.protocolHeaders, req.Header.Get("MCP-Protocol-Version"))
	r.mu.Unlock()
	if token != validToken {
		return nil, fmt.Errorf("unknown token: %w", auth.ErrInvalidToken)
	}
	return &auth.TokenInfo{UserID: "user-42"}, nil
}

func newTestServer(t *testing.T, rec *recorder, serverOpts []gomcp.Option, handlerOpts gomcp.HandlerOptions) *httptest.Server {
	t.Helper()
	echo := gomcp.Tool("echo", "Echoes the text back.",
		func(ctx context.Context, in echoIn) (echoOut, error) {
			userID, _ := gomcp.UserID(ctx)
			scoped, _ := ctx.Value(scopeKey{}).(string)
			return echoOut{Text: in.Text, UserID: userID, Scoped: scoped}, nil
		},
		gomcp.ReadOnly(), gomcp.Title("Echo"))
	fail := gomcp.Tool("fail", "Always fails.",
		func(ctx context.Context, in echoIn) (echoOut, error) {
			if in.Text == "business" {
				return echoOut{}, errBusiness
			}
			return echoOut{}, errors.New("pq: connection refused")
		},
		gomcp.Destructive(false), gomcp.Idempotent())
	write := gomcp.Tool("write", "Overwrites something.",
		func(ctx context.Context, in echoIn) (echoOut, error) { return echoOut{Text: in.Text}, nil })

	opts := append([]gomcp.Option{
		gomcp.WithPreInvoke(func(ctx context.Context, call gomcp.Call) (context.Context, error) {
			rec.mu.Lock()
			rec.preCalls = append(rec.preCalls, call)
			rec.mu.Unlock()
			if call.Caller == nil {
				return nil, errors.New("no caller")
			}
			return context.WithValue(ctx, scopeKey{}, "scope:"+call.Caller.UserID), nil
		}),
		gomcp.WithPostInvoke(func(_ context.Context, _ gomcp.Call, _ any, err error) {
			rec.mu.Lock()
			rec.postErrs = append(rec.postErrs, err)
			rec.mu.Unlock()
		}),
	}, serverOpts...)
	server := gomcp.NewServer("test", "v0.0.1", []gomcp.ToolDef{echo, fail, write}, opts...)

	if handlerOpts.Verifier == nil {
		handlerOpts.Verifier = rec.verify
	}
	handler, err := gomcp.Handler(server, handlerOpts)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return ts
}

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(req)
}

func connect(t *testing.T, url, token string) (*mcp.ClientSession, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   url,
		HTTPClient: &http.Client{Transport: bearerTransport{token: token}},
		MaxRetries: -1,
	}, nil)
	if err == nil {
		t.Cleanup(func() { _ = session.Close() })
	}
	return session, err
}

func mustConnect(t *testing.T, url string) *mcp.ClientSession {
	t.Helper()
	session, err := connect(t, url, validToken)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return session
}

func callTool(t *testing.T, session *mcp.ClientSession, name, text string) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      name,
		Arguments: map[string]any{"text": text},
	})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	return res
}

func textOf(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func listTools(t *testing.T) map[string]*mcp.Tool {
	t.Helper()
	ts := newTestServer(t, &recorder{}, nil, gomcp.HandlerOptions{})
	res, err := mustConnect(t, ts.URL).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	byName := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}
	if len(byName) != 3 {
		t.Fatalf("got %d tools, want 3", len(byName))
	}
	return byName
}

func TestListToolsShowsAnnotations(t *testing.T) {
	byName := listTools(t)

	echo := byName["echo"].Annotations
	if !echo.ReadOnlyHint || echo.Title != "Echo" {
		t.Errorf("echo annotations = %+v, want read-only titled Echo", echo)
	}
	fail := byName["fail"].Annotations
	if fail.DestructiveHint == nil || *fail.DestructiveHint || !fail.IdempotentHint {
		t.Errorf("fail annotations = %+v, want non-destructive idempotent", fail)
	}
	write := byName["write"].Annotations
	if write.DestructiveHint != nil || write.ReadOnlyHint {
		t.Errorf("write annotations = %+v, want MCP's destructive default", write)
	}
	if byName["echo"].InputSchema == nil || byName["echo"].OutputSchema == nil {
		t.Error("schemas must be inferred from In and Out")
	}
}

func TestToolsDeclareClosedWorld(t *testing.T) {
	for name, tool := range listTools(t) {
		if tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint {
			t.Errorf("%s: OpenWorldHint must default to false", name)
		}
	}
}

func TestCallerIdentityReachesHooksAndHandler(t *testing.T) {
	rec := &recorder{}
	ts := newTestServer(t, rec, nil, gomcp.HandlerOptions{})
	session := mustConnect(t, ts.URL)

	res := callTool(t, session, "echo", "hello")
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", textOf(res))
	}
	var out echoOut
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("structured content: %v", err)
	}
	want := echoOut{Text: "hello", UserID: "user-42", Scoped: "scope:user-42"}
	if out != want {
		t.Errorf("output = %+v, want %+v", out, want)
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.preCalls) != 1 {
		t.Fatalf("PreInvoke ran %d times, want 1", len(rec.preCalls))
	}
	call := rec.preCalls[0]
	if call.Tool != "echo" || call.Caller == nil || call.Caller.UserID != "user-42" {
		t.Errorf("PreInvoke call = %+v, want echo by user-42", call)
	}
	if in, ok := call.Input.(echoIn); !ok || in.Text != "hello" {
		t.Errorf("PreInvoke input = %#v, want echoIn{hello}", call.Input)
	}
	if len(rec.postErrs) != 1 || rec.postErrs[0] != nil {
		t.Errorf("PostInvoke errors = %v, want one nil", rec.postErrs)
	}
}

func TestServesProtocol20260728(t *testing.T) {
	rec := &recorder{}
	ts := newTestServer(t, rec, nil, gomcp.HandlerOptions{})
	session := mustConnect(t, ts.URL)
	callTool(t, session, "echo", "hi")

	rec.mu.Lock()
	defer rec.mu.Unlock()
	last := rec.protocolHeaders[len(rec.protocolHeaders)-1]
	if last != "2026-07-28" {
		t.Errorf("MCP-Protocol-Version = %q, want 2026-07-28", last)
	}
}

func TestHandlerErrorBecomesToolError(t *testing.T) {
	rec := &recorder{}
	ts := newTestServer(t, rec, nil, gomcp.HandlerOptions{})
	session := mustConnect(t, ts.URL)

	res := callTool(t, session, "fail", "internal")
	if !res.IsError {
		t.Fatal("want IsError result")
	}
	if got := textOf(res); got != "pq: connection refused" {
		t.Errorf("message = %q, want the handler's error by default", got)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.postErrs) != 1 || rec.postErrs[0] == nil {
		t.Errorf("PostInvoke must see the handler error, got %v", rec.postErrs)
	}
}

func TestErrorPolicyMasksInternalErrors(t *testing.T) {
	policy := gomcp.WithErrorPolicy(func(err error) string {
		if errors.Is(err, errBusiness) {
			return err.Error()
		}
		return "internal error"
	})
	rec := &recorder{}
	ts := newTestServer(t, rec, []gomcp.Option{policy}, gomcp.HandlerOptions{})
	session := mustConnect(t, ts.URL)

	if got := textOf(callTool(t, session, "fail", "internal")); got != "internal error" {
		t.Errorf("internal error shown as %q", got)
	}
	if got := textOf(callTool(t, session, "fail", "business")); got != "card not found" {
		t.Errorf("business error shown as %q", got)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.postErrs[0] == nil || rec.postErrs[0].Error() != "pq: connection refused" {
		t.Errorf("PostInvoke must see the unmasked error, got %v", rec.postErrs[0])
	}
}

func TestPreInvokeRejectionSkipsHandler(t *testing.T) {
	called := false
	tool := gomcp.Tool("guarded", "Guarded.", func(ctx context.Context, in echoIn) (echoOut, error) {
		called = true
		return echoOut{}, nil
	})
	server := gomcp.NewServer("test", "v0", []gomcp.ToolDef{tool},
		gomcp.WithPreInvoke(func(ctx context.Context, call gomcp.Call) (context.Context, error) {
			return nil, errors.New("rate limited")
		}))
	handler, err := gomcp.Handler(server, gomcp.HandlerOptions{Verifier: (&recorder{}).verify})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	res := callTool(t, mustConnect(t, ts.URL), "guarded", "x")
	if !res.IsError || textOf(res) != "rate limited" {
		t.Errorf("result = %+v, want IsError 'rate limited'", res)
	}
	if called {
		t.Error("handler ran despite PreInvoke rejection")
	}
}

func TestBearerRequired(t *testing.T) {
	ts := newTestServer(t, &recorder{}, nil, gomcp.HandlerOptions{
		ResourceMetadataURL: "https://api.example.com/.well-known/oauth-protected-resource/mcp",
	})

	for name, header := range map[string]string{"missing": "", "unknown": "Bearer fcm_revoked"} {
		t.Run(name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			if header != "" {
				req.Header.Set("Authorization", header)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", resp.StatusCode)
			}
			want := `Bearer resource_metadata="https://api.example.com/.well-known/oauth-protected-resource/mcp"`
			if got := resp.Header.Get("WWW-Authenticate"); got != want {
				t.Errorf("WWW-Authenticate = %q, want %q", got, want)
			}
		})
	}

	if _, err := connect(t, ts.URL, "fcm_revoked"); err == nil {
		t.Error("client connected with a revoked token")
	}
}

func TestOriginCheck(t *testing.T) {
	ts := newTestServer(t, &recorder{}, nil, gomcp.HandlerOptions{AllowedOrigins: []string{"https://app.example.com"}})

	post := func(origin string) int {
		req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+validToken)
		req.Header.Set("MCP-Protocol-Version", "2025-11-25")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	if got := post("https://evil.example.com"); got != http.StatusForbidden {
		t.Errorf("foreign origin: status = %d, want 403", got)
	}
	if got := post(ts.URL); got != http.StatusForbidden {
		t.Errorf("same-host origin not listed: status = %d, want 403", got)
	}
	if got := post("https://app.example.com"); got == http.StatusForbidden {
		t.Errorf("allowed origin refused with 403")
	}
	if got := post(""); got == http.StatusForbidden {
		t.Errorf("request without Origin refused with 403")
	}
}

func TestLocalhostProtection(t *testing.T) {
	post := func(ts *httptest.Server) int {
		req, _ := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		req.Host = "abc.trycloudflare.com"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+validToken)
		req.Header.Set("MCP-Protocol-Version", "2025-11-25")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	guarded := newTestServer(t, &recorder{}, nil, gomcp.HandlerOptions{})
	if got := post(guarded); got != http.StatusForbidden {
		t.Errorf("tunnel Host on loopback: status = %d, want 403", got)
	}
	tunnelled := newTestServer(t, &recorder{}, nil, gomcp.HandlerOptions{DisableLocalhostProtection: true})
	if got := post(tunnelled); got != http.StatusOK {
		t.Errorf("tunnel Host with protection disabled: status = %d, want 200", got)
	}
}

func TestHandlerRequiresVerifier(t *testing.T) {
	server := gomcp.NewServer("test", "v0", nil)
	if _, err := gomcp.Handler(server, gomcp.HandlerOptions{}); err == nil {
		t.Error("Handler accepted options without a Verifier")
	}
	if _, err := gomcp.Handler(nil, gomcp.HandlerOptions{Verifier: (&recorder{}).verify}); err == nil {
		t.Error("Handler accepted a nil server")
	}
}

func TestDuplicateToolNamePanics(t *testing.T) {
	tool := gomcp.Tool("same", "x", func(ctx context.Context, in echoIn) (echoOut, error) { return echoOut{}, nil })
	defer func() {
		if recover() == nil {
			t.Error("NewServer accepted two tools with one name")
		}
	}()
	gomcp.NewServer("test", "v0", []gomcp.ToolDef{tool, tool})
}

func TestUserIDOutsideCall(t *testing.T) {
	if id, ok := gomcp.UserID(context.Background()); ok || id != "" {
		t.Errorf("UserID outside a call = (%q, %v), want (\"\", false)", id, ok)
	}
}
