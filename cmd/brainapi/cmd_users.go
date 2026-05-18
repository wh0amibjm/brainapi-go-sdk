package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func newUsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users",
		Short: "User profile / activity endpoints",
	}
	cmd.AddCommand(newUsersSelfCmd(), newUsersCompetitionsCmd(), newUsersActivitiesCmd())
	return cmd
}

func newUsersSelfCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "self",
		Short: "GET /users/self",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			u, err := cl.Self(ctx)
			if err != nil {
				writeErr(err)
				return nil
			}
			writeOK(u)
			return nil
		},
	}
}

func newUsersCompetitionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "competitions",
		Short: "GET /users/self/competitions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			p, err := cl.Competitions(ctx)
			if err != nil {
				writeErr(err)
				return nil
			}
			writeOK(p)
			return nil
		},
	}
}

func newUsersActivitiesCmd() *cobra.Command {
	var decode bool
	cmd := &cobra.Command{
		Use:   "activities <kind>",
		Short: "GET /users/self/activities/{kind}: base-payment | other-payment | simulations | submissions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind := brainapi.ActivityKind(args[0])
			switch kind {
			case brainapi.ActivityBasePayment, brainapi.ActivityOtherPayment,
				brainapi.ActivitySimulations, brainapi.ActivitySubmissions:
			default:
				writeErr(fmt.Errorf("unknown activity kind %q; must be base-payment|other-payment|simulations|submissions", args[0]))
				return nil
			}
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			s, err := cl.Activities(ctx, kind)
			if err != nil {
				writeErr(err)
				return nil
			}
			if decode {
				recs, err := brainapi.DecodeActivities(s)
				if err != nil {
					writeErr(err)
					return nil
				}
				writeOK(map[string]any{
					"type":      s.Type,
					"currency":  s.Currency,
					"yesterday": s.Yesterday,
					"current":   s.Current,
					"previous":  s.Previous,
					"ytd":       s.YTD,
					"total":     s.Total,
					"records":   recs,
				})
				return nil
			}
			writeOK(s)
			return nil
		},
	}
	cmd.Flags().BoolVar(&decode, "decode", false, "Decode tuple-array records into named-column dicts")
	return cmd
}
