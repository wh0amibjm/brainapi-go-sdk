package main

import (
	"context"
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func newSchemaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Static schema endpoints (operators, data-fields)",
	}
	cmd.AddCommand(
		newSchemaOperatorsCmd(),
		newSchemaDataFieldsCmd(),
		newSchemaSimulationOptionsCmd(),
		newSchemaDatasetsCmd(),
		newSchemaThemesCmd(),
	)
	return cmd
}

func newSchemaDatasetsCmd() *cobra.Command {
	q := brainapi.DataFieldsQuery{
		InstrumentType: "EQUITY",
		Region:         "USA",
		Universe:       "TOP3000",
		Delay:          1,
	}
	var all bool
	cmd := &cobra.Command{
		Use:   "datasets",
		Short: "GET /data-sets: dataset catalog with consultant Dataset Value Score + pyramid multiplier (paginated). Path/field names inferred — verify via live probe.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()
			if !all {
				page, err := cl.Datasets(ctx, q)
				if err != nil {
					writeErr(err)
					return nil
				}
				writeOK(page)
				return nil
			}
			sets, err := drainAll(cl.DatasetsAll(ctx, q))
			if err != nil {
				writeErr(err)
				return nil
			}
			writeOK(map[string]any{"count": len(sets), "results": sets})
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

func newSchemaThemesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "themes",
		Short: "GET /themes: currently-announced consultant Themes + QualityFactor multipliers. Path/schema inferred — verify via live probe; degrades on 404/403.",
		RunE: runE(func(cl *brainapi.Client, ctx context.Context) ([]brainapi.Theme, error) {
			return cl.Themes(ctx)
		}),
	}
}

func newSchemaSimulationOptionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "simulation-options",
		Short: "OPTIONS /simulations: dynamic DRF metadata schema (settable fields + enum choices)",
		RunE: runE(func(cl *brainapi.Client, ctx context.Context) (map[string]json.RawMessage, error) {
			return cl.SimulationOptions(ctx)
		}),
	}
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
			fields, err := drainAll(cl.DataFieldsAll(ctx, q))
			if err != nil {
				writeErr(err)
				return nil
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
