package main

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authentication endpoints (login, probe, logout)",
	}
	cmd.AddCommand(newAuthLoginCmd(), newAuthProbeCmd(), newAuthLogoutCmd())
	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "POST /authentication: log in with Basic auth credentials",
		RunE: runE(func(cl *brainapi.Client, ctx context.Context) (*brainapi.Session, error) {
			user := firstNonEmpty(gf.email, os.Getenv("BRAINAPI_USER"))
			pass := firstNonEmpty(gf.password, os.Getenv("BRAINAPI_PASS"))
			return cl.Login(ctx, user, pass)
		}),
	}
}

func newAuthProbeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "probe",
		Short: "GET /authentication: return live session info",
		RunE: runE(func(cl *brainapi.Client, ctx context.Context) (*brainapi.SessionInfo, error) {
			return cl.Probe(ctx)
		}),
	}
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "DELETE /authentication: sign out",
		RunE: runE(func(cl *brainapi.Client, ctx context.Context) (map[string]bool, error) {
			if err := cl.Logout(ctx); err != nil {
				return nil, err
			}
			return map[string]bool{"signed_out": true}, nil
		}),
	}
}
