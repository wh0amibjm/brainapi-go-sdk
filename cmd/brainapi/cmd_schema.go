package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func newSchemaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Static schema endpoints (operators, data-fields)",
	}
	cmd.AddCommand(newSchemaOperatorsCmd(), newSchemaDataFieldsCmd())
	return cmd
}

func newSchemaOperatorsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "operators",
		Short: "GET /operators: full operator catalog (bare array)",
		RunE: runE(func(cl *brainapi.Client, ctx context.Context) ([]brainapi.Operator, error) {
			return cl.Operators(ctx)
		}),
	}
}

func newSchemaDataFieldsCmd() *cobra.Command {
	q := brainapi.DataFieldsQuery{
		InstrumentType: "EQUITY",
		Region:         "USA",
		Universe:       "TOP3000",
		Delay:          1,
	}
	var all bool
	cmd := &cobra.Command{
		Use:   "data-fields",
		Short: "GET /data-fields: data-field catalog (paginated, count+results)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			if !all {
				page, err := cl.DataFields(ctx, q)
				if err != nil {
					writeErr(err)
					return nil
				}
				writeOK(page)
				return nil
			}
			out, errs := cl.DataFieldsAll(ctx, q)
			var fields []brainapi.DataField
			for {
				select {
				case f, ok := <-out:
					if !ok {
						out = nil
					} else {
						fields = append(fields, f)
					}
				case e, ok := <-errs:
					if !ok {
						errs = nil
					} else if e != nil {
						writeErr(e)
						return nil
					}
				}
				if out == nil && errs == nil {
					break
				}
			}
			writeOK(map[string]any{"count": len(fields), "results": fields})
			return nil
		},
	}
	cmd.Flags().StringVar(&q.InstrumentType, "instrument-type", q.InstrumentType, "BRAIN instrumentType (required)")
	cmd.Flags().StringVar(&q.Region, "region", q.Region, "Region (required)")
	cmd.Flags().StringVar(&q.Universe, "universe", q.Universe, "Universe (required)")
	cmd.Flags().IntVar(&q.Delay, "delay", q.Delay, "Delay 0 or 1 (required)")
	cmd.Flags().IntVar(&q.Limit, "limit", 0, "Page size")
	cmd.Flags().IntVar(&q.Offset, "offset", 0, "Pagination offset")
	cmd.Flags().BoolVar(&all, "all", false, "Drain all pages")
	return cmd
}
