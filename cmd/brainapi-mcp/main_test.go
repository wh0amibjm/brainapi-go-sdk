package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

// readTools must always be registered, regardless of --enable-writes. This
// also covers report_issue: not a BRAIN GET, but the always-on agent feedback
// channel, gated by its own confirm flag rather than by --enable-writes.
var readTools = []string{
	"probe", "whoami", "competitions", "operators", "get_alpha", "check_alpha",
	"check_alpha_decoded", "self_correlation", "alpha_pnl", "performance",
	"activities", "list_alphas", "list_alphas_all", "messages", "messages_all",
	"data_fields", "data_fields_all", "get_simulation", "wait_simulation",
	"simulations_wait_multi", "captcha_challenge", "report_issue",
}

// writeTools are mutating/scarce operations gated behind --enable-writes.
var writeTools = []string{
	"submit_alpha", "simulations_create", "simulations_create_multi",
	"register", "login", "logout",
	"email_verify", "email_reverify", "password_forgot", "password_reset",
	"persona_complete",
}

// listToolNames spins up the server over an in-memory transport, connects a
// client, and returns the set of advertised tool names. No network, no creds.
func listToolNames(t *testing.T, enableWrites bool) map[string]bool {
	t.Helper()

	cl, err := brainapi.NewClient(brainapi.Options{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientT, serverT := mcp.NewInMemoryTransports()

	ss, err := newServer(cl, enableWrites).Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).
		Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	names := make(map[string]bool, len(res.Tools))
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	return names
}

// TestReadOnlyByDefault: without --enable-writes, all read tools are present
// and every mutating tool is absent.
func TestReadOnlyByDefault(t *testing.T) {
	names := listToolNames(t, false)

	for _, want := range readTools {
		if !names[want] {
			t.Errorf("read tool %q missing in default mode", want)
		}
	}
	for _, w := range writeTools {
		if names[w] {
			t.Errorf("write tool %q must be gated behind --enable-writes, but it is registered by default", w)
		}
	}
	if got := len(names); got != len(readTools) {
		t.Errorf("default mode tool count = %d, want %d", got, len(readTools))
	}
}

// TestEnableWrites: with --enable-writes, the mutating tools join the read tools.
func TestEnableWrites(t *testing.T) {
	names := listToolNames(t, true)

	for _, want := range append(append([]string{}, readTools...), writeTools...) {
		if !names[want] {
			t.Errorf("tool %q missing with --enable-writes", want)
		}
	}
	if got, want := len(names), len(readTools)+len(writeTools); got != want {
		t.Errorf("--enable-writes tool count = %d, want %d", got, want)
	}
}

// TestServerInstructions: the server advertises operating instructions at
// initialize (auth, error handling, safety) so an agent gets the contract
// before calling any tool — independent of which tool it calls first.
func TestServerInstructions(t *testing.T) {
	cl, err := brainapi.NewClient(brainapi.Options{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientT, serverT := mcp.NewInMemoryTransports()
	ss, err := newServer(cl, false).Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).
		Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	instr := cs.InitializeResult().Instructions
	for _, want := range []string{"not_authenticated", "BRAINAPI_USER", "confirm=true"} {
		if !strings.Contains(instr, want) {
			t.Errorf("server instructions missing %q", want)
		}
	}
}

// TestClassifyErr: SDK errors are wrapped into a structuredErr whose kind comes
// from brainapi.Classify and whose Error() is machine-parseable JSON, so an
// agent can branch on the kind rather than parsing English.
func TestClassifyErr(t *testing.T) {
	t.Parallel()

	if classifyErr(nil) != nil {
		t.Fatal("classifyErr(nil) should be nil")
	}

	wrapped := fmt.Errorf("submit failed: %w", &brainapi.RateLimitError{Status: 429, RetryAfter: 5 * time.Second})
	var se *structuredErr
	if !errors.As(classifyErr(wrapped), &se) {
		t.Fatalf("classifyErr returned %T, want *structuredErr", classifyErr(wrapped))
	}
	if se.Kind != "rate_limit" {
		t.Errorf("kind = %q, want rate_limit", se.Kind)
	}
	if se.Details["retry_after_ms"] != int64(5000) {
		t.Errorf("details.retry_after_ms = %v, want 5000", se.Details["retry_after_ms"])
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(se.Error()), &got); err != nil {
		t.Fatalf("Error() is not JSON-parseable: %v", err)
	}
	if got["kind"] != "rate_limit" {
		t.Errorf("Error() JSON kind = %v, want rate_limit", got["kind"])
	}
}

// TestToolErrorIsStructured drives a real tool call over the in-memory transport
// against an unreachable base URL and asserts the failure reaches the agent as
// an IsError result whose text content is a structured {kind,...} JSON payload.
func TestToolErrorIsStructured(t *testing.T) {
	// MaxRetries:1 keeps the connection-refused failure fast (one backoff) so the
	// call resolves to a tool result well within the context deadline.
	cl, err := brainapi.NewClient(brainapi.Options{BaseURL: "http://127.0.0.1:1", MaxRetries: 1, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientT, serverT := mcp.NewInMemoryTransports()
	ss, err := newServer(cl, false).Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).
		Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "operators"})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for an unreachable base URL")
	}
	if len(res.Content) == 0 {
		t.Fatal("expected error content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want *mcp.TextContent", res.Content[0])
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(tc.Text), &got); err != nil {
		t.Fatalf("error content is not structured JSON: %v (text=%q)", err, tc.Text)
	}
	if k, _ := got["kind"].(string); k == "" {
		t.Errorf("structured error missing kind: %v", got)
	}
}
