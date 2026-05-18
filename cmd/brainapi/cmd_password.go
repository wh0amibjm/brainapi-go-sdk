package main

import (
	"github.com/spf13/cobra"
)

func newPasswordCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "password",
		Short: "Password-reset endpoints (forgot, reset)",
	}
	cmd.AddCommand(newPasswordForgotCmd(), newPasswordResetCmd())
	return cmd
}

func newPasswordForgotCmd() *cobra.Command {
	var email, recaptcha string
	cmd := &cobra.Command{
		Use:   "forgot --email <addr> [--recaptcha <token>]",
		Short: "POST /user/password/forgot: initiate password reset flow",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			if err := cl.ForgotPassword(ctx, email, recaptcha); err != nil {
				writeErr(err)
				return nil
			}
			writeOK(map[string]bool{"reset_initiated": true})
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "Email to send reset mail to (required)")
	cmd.Flags().StringVar(&recaptcha, "recaptcha", "", "Legacy reCAPTCHA token (leave empty if BRAIN dropped the field)")
	return cmd
}

func newPasswordResetCmd() *cobra.Command {
	var jwt, password string
	cmd := &cobra.Command{
		Use:   "reset --jwt <token> --password <new>",
		Short: "POST /user/password/reset: complete password reset with JWT + new password",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			if err := cl.ResetPassword(ctx, jwt, password); err != nil {
				writeErr(err)
				return nil
			}
			writeOK(map[string]bool{"reset": true})
			return nil
		},
	}
	cmd.Flags().StringVar(&jwt, "jwt", "", "JWT from reset-mail link's ?token= param (required)")
	cmd.Flags().StringVar(&password, "password", "", "New password (required)")
	return cmd
}
