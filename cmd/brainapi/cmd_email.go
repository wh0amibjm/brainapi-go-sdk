package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
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
		RunE: runE(func(cl *brainapi.Client, ctx context.Context) (map[string]bool, error) {
			if err := cl.ReverifyEmail(ctx, email, recaptcha); err != nil {
				return nil, err
			}
			return map[string]bool{"resent": true}, nil
		}),
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
		RunE: runE(func(cl *brainapi.Client, ctx context.Context) (map[string]bool, error) {
			if err := cl.VerifyEmail(ctx, jwt); err != nil {
				return nil, err
			}
			return map[string]bool{"verified": true}, nil
		}),
	}
	cmd.Flags().StringVar(&jwt, "jwt", "", "JWT from email verification link's ?token= param (required)")
	return cmd
}
