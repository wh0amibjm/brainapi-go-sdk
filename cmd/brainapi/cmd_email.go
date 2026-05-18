package main

import (
	"github.com/spf13/cobra"
)

func newEmailCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "email",
		Short: "Email-verification endpoints (verify, reverify)",
	}
	cmd.AddCommand(newEmailReverifyCmd(), newEmailVerifyCmd())
	return cmd
}

func newEmailReverifyCmd() *cobra.Command {
	var email, recaptcha string
	cmd := &cobra.Command{
		Use:   "reverify --email <addr> [--recaptcha <token>]",
		Short: "POST /user/email/reverify: resend verification email (anonymous mode)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			if err := cl.ReverifyEmail(ctx, email, recaptcha); err != nil {
				writeErr(err)
				return nil
			}
			writeOK(map[string]bool{"resent": true})
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "Address to resend verification mail to (required)")
	cmd.Flags().StringVar(&recaptcha, "recaptcha", "", "Legacy reCAPTCHA token (leave empty if BRAIN dropped the field)")
	return cmd
}

func newEmailVerifyCmd() *cobra.Command {
	var jwt string
	cmd := &cobra.Command{
		Use:   "verify --jwt <token>",
		Short: "POST /user/email/verify: confirm email with JWT from verification link",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			if err := cl.VerifyEmail(ctx, jwt); err != nil {
				writeErr(err)
				return nil
			}
			writeOK(map[string]bool{"verified": true})
			return nil
		},
	}
	cmd.Flags().StringVar(&jwt, "jwt", "", "JWT from email verification link's ?token= param (required)")
	return cmd
}
