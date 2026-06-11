package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func newAlphasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alphas",
		Short: "Alpha endpoints (get, check, submit, pnl, corr, list)",
	}
	cmd.AddCommand(
		newAlphaGetCmd(),
		newAlphaCheckCmd(),
		newAlphaSubmitCmd(),
		newAlphaPnLCmd(),
		newAlphaCorrCmd(),
		newAlphaCorrLocalCmd(),
		newAlphaListCmd(),
		newAlphaPerformanceCmd(),
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

func newAlphaCorrCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "corr <alpha-id>",
		Short: "GET /alphas/{id}/correlations/self: pre-submit corr check (gate SubmitAlpha on max<0.7)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			b, err := cl.AlphaSelfCorrelation(ctx, args[0])
			if err != nil {
				writeErr(err)
				return nil
			}
			writeOK(b)
			return nil
		},
	}
}

func newAlphaCorrLocalCmd() *cobra.Command {
	var jsonFlag string
	cmd := &cobra.Command{
		Use:   "corr-local --json <file|->",
		Short: "Local self-correlation (offline, no BRAIN call): Pearson over trailing-4y PnL returns of a candidate vs supplied neighbours",
		Long: "Compute self-correlation OFFLINE from PnL series you already have — a pure-Go reimplementation of BRAIN's GET /alphas/{id}/correlations/self semantics.\n\n" +
			"Body (--json file path or '-' for stdin):\n" +
			`  {"candidate":{"id":"X","records":[["2020-01-02",1234.5],...]},` + "\n" +
			`   "neighbours":[{"id":"Y","records":[["2020-01-02",10.0],...]},...]}` + "\n\n" +
			"records are [date, cumulativePnl] tuples (same shape as `alphas pnl`). Both candidate and neighbours are trimmed to the trailing 4 years and converted to daily returns; any neighbour whose id equals the candidate's is excluded. Output: {corrMax, neighbours:[{id,corr,overlap}], considered, skipped}. corrMax is signed and ranked by |corr|; gate submission on |corrMax| < 0.7.",
		RunE: func(_ *cobra.Command, _ []string) error {
			if jsonFlag == "" {
				writeErr(fmt.Errorf("--json is required"))
				return nil
			}
			body, err := readBody(jsonFlag)
			if err != nil {
				writeErr(err)
				return nil
			}
			var in brainapi.SelfCorrLocalInput
			if err := json.Unmarshal(body, &in); err != nil {
				writeErr(fmt.Errorf("parse corr-local body: %w", err))
				return nil
			}
			writeOK(brainapi.SelfCorrLocal(in))
			return nil
		},
	}
	cmd.Flags().StringVar(&jsonFlag, "json", "", "Candidate + neighbours JSON: file path or '-' for stdin")
	return cmd
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

func newAlphaPerformanceCmd() *cobra.Command {
	var competition string
	cmd := &cobra.Command{
		Use:   "performance <alpha-id> --competition <id>",
		Short: "GET /competitions/{cid}/alphas/{id}/before-and-after-performance: projected submit impact (competition score + stats, before vs after)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if competition == "" {
				writeErr(fmt.Errorf("--competition is required (e.g. IQC2026S2)"))
				return nil
			}
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			p, err := cl.BeforeAndAfterPerformance(ctx, competition, args[0])
			if err != nil {
				writeErr(err)
				return nil
			}
			writeOK(p)
			return nil
		},
	}
	cmd.Flags().StringVar(&competition, "competition", "", "Competition id the alpha would be submitted to, e.g. IQC2026S2 (required)")
	return cmd
}

func newAlphaListCmd() *cobra.Command {
	var status, order string
	var limit, offset int
	var all bool
	var filters []string
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
					Status: status, Order: order, Limit: limit, Offset: offset, Filters: filters,
				})
				if err != nil {
					writeErr(err)
					return nil
				}
				writeOK(page)
				return nil
			}

			alphas, err := drainAll(cl.ListAlphasAll(ctx, brainapi.ListAlphasOptions{
				Status: status, Order: order, Limit: limit, Offset: offset, Filters: filters,
			}))
			if err != nil {
				writeErr(err)
				return nil
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
	cmd.Flags().StringArrayVar(&filters, "filter", nil,
		"BRAIN comparison filter, repeatable (AND); operator embedded in the field, "+
			"e.g. --filter 'is.sharpe>=1.25' --filter 'is.turnover<=0.7'")
	return cmd
}
