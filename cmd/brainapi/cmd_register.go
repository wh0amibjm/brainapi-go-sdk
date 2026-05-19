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
		Long: `Register a secondary account via POST /users. The Altcha PoW captcha is
auto-solved by the SDK's parallel SHA-256 solver and injected into
auxiliary.captcha — no manual challenge handling required.

Minimal valid RegisterInput (note: graduationYear, NOT gradYear; no address.zip):

  {
    "email":     "you@example.com",
    "firstName": "Test",
    "lastName":  "Account",
    "fullName":  "Test Account",
    "gender":    "MALE",
    "address":   {"country": "US"},
    "education": {"university": "MIT", "major": "CS", "degree": "BACHELORS", "graduationYear": 2026},
    "auxiliary": {"password": "S3cure!!"}
  }

Education.degree must be one of BACHELORS / MASTERS / ASSOCIATE — BRAIN
rejects other values with a DRF validation error. See docs/protocol.md
for the full field list and the SendGrid email-verify caveat.

End-to-end live test: scripts/register (also reachable via
` + "`make test-register`" + `) auto-generates a @example.com account and
exercises register -> login -> probe -> self.`,
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
