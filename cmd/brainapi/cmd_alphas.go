package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func newAlphasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alphas",
		Short: "Alpha endpoints (get, check, submit, pnl, list)",
	}
	cmd.AddCommand(
		newAlphaGetCmd(),
		newAlphaCheckCmd(),
		newAlphaSubmitCmd(),
		newAlphaPnLCmd(),
		newAlphaListCmd(),
	)
	return cmd
}

func newAlphaGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <alpha-id>",
		Short: "GET /alphas/{id}: fetch full alpha record",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			a, err := cl.GetAlpha(ctx, args[0])
			if err != nil {
				writeErr(err)
				return nil
			}
			writeOK(a)
			return nil
		},
	}
}

func newAlphaCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "check <alpha-id>",
		Short: "GET /alphas/{id}/check: long-poll pre-submit validations",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			is, err := cl.CheckAlpha(ctx, args[0])
			if err != nil {
				writeErr(err)
				return nil
			}
			writeOK(is)
			return nil
		},
	}
}

func newAlphaSubmitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "submit <alpha-id>",
		Short: "POST /alphas/{id}/submit + long-poll for verdict",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			v, err := cl.SubmitAlpha(ctx, args[0])
			if err != nil {
				writeErr(err)
				return nil
			}
			writeOK(v)
			return nil
		},
	}
}

func newAlphaPnLCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pnl <alpha-id>",
		Short: "GET /alphas/{id}/recordsets/pnl: PnL series (long-poll cold cache)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			s, err := cl.AlphaPnL(ctx, args[0])
			if err != nil {
				writeErr(err)
				return nil
			}
			writeOK(s)
			return nil
		},
	}
}

func newAlphaListCmd() *cobra.Command {
	var status, order string
	var limit, offset int
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "GET /users/self/alphas: paginated alpha list",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()

			if !all {
				page, err := cl.ListAlphas(ctx, brainapi.ListAlphasOptions{
					Status: status, Order: order, Limit: limit, Offset: offset,
				})
				if err != nil {
					writeErr(err)
					return nil
				}
				writeOK(page)
				return nil
			}

			out, errs := cl.ListAlphasAll(ctx, brainapi.ListAlphasOptions{
				Status: status, Order: order, Limit: limit, Offset: offset,
			})
			var alphas []brainapi.Alpha
			for {
				select {
				case a, ok := <-out:
					if !ok {
						out = nil
					} else {
						alphas = append(alphas, a)
					}
				case e, ok := <-errs:
					if !ok {
						errs = nil
					} else if e != nil {
						writeErr(fmt.Errorf("paginate: %w", e))
						return nil
					}
				}
				if out == nil && errs == nil {
					break
				}
			}
			writeOK(map[string]any{"count": len(alphas), "results": alphas})
			return nil
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "Filter: ACTIVE|UNSUBMITTED|DECOMMISSIONED")
	cmd.Flags().StringVar(&order, "order", "", "Sort key, e.g. -dateCreated")
	cmd.Flags().IntVar(&limit, "limit", 100, "Page size")
	cmd.Flags().IntVar(&offset, "offset", 0, "Pagination offset")
	cmd.Flags().BoolVar(&all, "all", false, "Drain all pages (default: first page only)")
	return cmd
}
