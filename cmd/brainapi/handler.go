package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

// runE wraps the standard endpoint-command flow: build a Client from the
// global flags, derive a signal-cancellable context, invoke fn, and route the
// outcome through writeErr / writeOK. Commands with extra logic (pagination
// drains, arg parsing, custom output) keep their own RunE.
func runE[T any](fn func(*brainapi.Client, context.Context) (T, error)) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		cl, err := newClient(cmd)
		if err != nil {
			writeErr(err)
			return nil
		}
		ctx, cancel := ctxWithSignal()
		defer cancel()
		res, err := fn(cl, ctx)
		if err != nil {
			writeErr(err)
			return nil
		}
		writeOK(res)
		return nil
	}
}
