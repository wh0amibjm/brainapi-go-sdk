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
	cmd.AddCommand(newSimCreateCmd(), newSimGetCmd(), newSimWaitCmd())
	return cmd
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
			id, err := cl.CreateSimulation(ctx, req)
			if err != nil {
				writeErr(err)
				return nil
			}
			writeOK(map[string]string{"id": id})
			return nil
		},
	}
	cmd.Flags().StringVar(&jsonFlag, "json", "", "Simulation request JSON: file path or '-' for stdin")
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
