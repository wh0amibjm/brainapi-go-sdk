package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func newSimulationsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "simulations",
		Aliases: []string{"sim"},
		Short:   "Simulation endpoints (create, get, wait)",
	}
	cmd.AddCommand(
		newSimCreateCmd(),
		newSimCreateMultiCmd(),
		newSimGetCmd(),
		newSimWaitCmd(),
		newSimWaitMultiCmd(),
	)
	return cmd
}

// simCreateEnvelope keeps the {"id": ...} shape existing callers parse and
// ADDS a "rateLimit" object (X-Ratelimit-* daily-sim-quota snapshot) alongside
// it — backward-compatible.
func simCreateEnvelope(r *brainapi.CreateSimulationResult) map[string]any {
	return map[string]any{
		"id": r.ID,
		"rateLimit": map[string]any{
			"limit":        r.RateLimit.Limit,
			"remaining":    r.RateLimit.Remaining,
			"resetSeconds": int(r.RateLimit.Reset.Seconds()),
			"present":      r.RateLimit.Present,
		},
	}
}

func newSimCreateCmd() *cobra.Command {
	var jsonFlag string
	cmd := &cobra.Command{
		Use:   "create --json <file|->",
		Short: "POST /simulations: enqueue a backtest. Body comes from --json (file path or '-' for stdin)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if jsonFlag == "" {
				writeErr(fmt.Errorf("--json is required"))
				return nil
			}
			body, err := readBody(jsonFlag)
			if err != nil {
				writeErr(err)
				return nil
			}
			var req brainapi.SimulationRequest
			if err := json.Unmarshal(body, &req); err != nil {
				writeErr(fmt.Errorf("parse simulation body: %w", err))
				return nil
			}
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			res, err := cl.CreateSimulation(ctx, req)
			if err != nil {
				writeErr(err)
				return nil
			}
			writeOK(simCreateEnvelope(res))
			return nil
		},
	}
	cmd.Flags().StringVar(&jsonFlag, "json", "", "Simulation request JSON: file path or '-' for stdin")
	return cmd
}

// newSimCreateMultiCmd POSTs an ARRAY of 2..10 simulation objects
// (MULTI_SIMULATION). Returns the PARENT id + the rate-limit snapshot; poll it
// with `simulations wait-multi`.
func newSimCreateMultiCmd() *cobra.Command {
	var jsonFlag string
	cmd := &cobra.Command{
		Use:   "create-multi --json <file|->",
		Short: "POST /simulations (array 2..10): enqueue a multi-simulation. Returns the parent id.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if jsonFlag == "" {
				writeErr(fmt.Errorf("--json is required"))
				return nil
			}
			body, err := readBody(jsonFlag)
			if err != nil {
				writeErr(err)
				return nil
			}
			var reqs []brainapi.SimulationRequest
			if err := json.Unmarshal(body, &reqs); err != nil {
				writeErr(fmt.Errorf("parse multi-simulation body (expected a JSON array): %w", err))
				return nil
			}
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			res, err := cl.CreateMultiSimulation(ctx, reqs)
			if err != nil {
				writeErr(err)
				return nil
			}
			writeOK(simCreateEnvelope(res))
			return nil
		},
	}
	cmd.Flags().StringVar(&jsonFlag, "json", "", "Multi-simulation request JSON array: file path or '-' for stdin")
	return cmd
}

func newSimGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <sim-id>",
		Short: "GET /simulations/{id}: one-shot status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			s, err := cl.GetSimulation(ctx, args[0])
			if err != nil {
				writeErr(err)
				return nil
			}
			writeOK(s)
			return nil
		},
	}
}

func newSimWaitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "wait <sim-id>",
		Short: "Poll GET /simulations/{id} until terminal (COMPLETE|FAIL|ERROR)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			s, err := cl.WaitForSimulation(ctx, args[0])
			if err != nil {
				writeErr(err)
				return nil
			}
			writeOK(s)
			return nil
		},
	}
}

// newSimWaitMultiCmd polls a multi-simulation PARENT to terminal, then resolves
// every child, emitting {"parent": <sim>, "children": [<sim>, ...]}.
func newSimWaitMultiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "wait-multi <parent-sim-id>",
		Short: "Poll a multi-simulation parent to terminal, then return the parent and all child simulations",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			parent, children, err := cl.WaitForMultiSimulation(ctx, args[0])
			if parent == nil {
				// Couldn't resolve the parent itself — a hard failure.
				writeErr(err)
				return nil
			}
			out := map[string]any{"parent": parent, "children": children}
			if err != nil {
				// Parent resolved but ≥1 child is still pending/unreachable:
				// surface every already-minted child so nothing is lost, flagged
				// incomplete so the caller re-polls the missing ones.
				out["incomplete"] = true
				out["error"] = err.Error()
			}
			writeOK(out)
			return nil
		},
	}
}

// readBody reads a body from --json: '-' = stdin, '@path' = file (curl style),
// 'path' = file, anything else = literal.
func readBody(spec string) ([]byte, error) {
	if spec == "-" {
		return readAllStdin()
	}
	if strings.HasPrefix(spec, "@") {
		return os.ReadFile(spec[1:])
	}
	if _, err := os.Stat(spec); err == nil {
		return os.ReadFile(spec)
	}
	return []byte(spec), nil
}

func readAllStdin() ([]byte, error) {
	var b []byte
	buf := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			b = append(b, buf[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" {
				return b, nil
			}
			return b, nil
		}
	}
}
