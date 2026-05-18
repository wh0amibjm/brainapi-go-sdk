package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func newRegisterCmd() *cobra.Command {
	var jsonFlag string
	cmd := &cobra.Command{
		Use:   "register --json <file|->",
		Short: "POST /users: register a secondary account. Captcha auto-solved via Altcha PoW.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if jsonFlag == "" {
				writeErr(fmt.Errorf("--json is required (RegisterInput body)"))
				return nil
			}
			body, err := readBody(jsonFlag)
			if err != nil {
				writeErr(err)
				return nil
			}
			var in brainapi.RegisterInput
			if err := json.Unmarshal(body, &in); err != nil {
				writeErr(fmt.Errorf("parse register body: %w", err))
				return nil
			}
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			u, err := cl.Register(ctx, in)
			if err != nil {
				writeErr(err)
				return nil
			}
			writeOK(map[string]any{"registered": true, "user": u})
			return nil
		},
	}
	cmd.Flags().StringVar(&jsonFlag, "json", "", "RegisterInput JSON: file path, '@file', or '-' for stdin")
	return cmd
}
