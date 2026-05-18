package main

import (
	"github.com/spf13/cobra"
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			user := firstNonEmpty(gf.email, "")
			pass := firstNonEmpty(gf.password, "")
			sess, err := cl.Login(ctx, user, pass)
			if err != nil {
				writeErr(err)
				return nil
			}
			writeOK(sess)
			return nil
		},
	}
}

func newAuthProbeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "probe",
		Short: "GET /authentication: return live session info",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			info, err := cl.Probe(ctx)
			if err != nil {
				writeErr(err)
				return nil
			}
			writeOK(info)
			return nil
		},
	}
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "DELETE /authentication: sign out",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			if err := cl.Logout(ctx); err != nil {
				writeErr(err)
				return nil
			}
			writeOK(map[string]bool{"signed_out": true})
			return nil
		},
	}
}
