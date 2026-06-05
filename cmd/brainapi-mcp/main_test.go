package main

import (
	"context"
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
	"captcha_challenge", "report_issue",
}

// writeTools are mutating/scarce operations gated behind --enable-writes.
var writeTools = []string{
	"submit_alpha", "simulations_create", "register", "login", "logout",
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
