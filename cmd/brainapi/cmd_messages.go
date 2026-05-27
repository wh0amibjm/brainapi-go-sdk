package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func newMessagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "messages",
		Short: "Notification feed (announcements, dataset updates, ...)",
	}
	cmd.AddCommand(newMessagesListCmd())
	return cmd
}

func newMessagesListCmd() *cobra.Command {
	var typ, order string
	var limit, offset int
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "GET /users/self/messages: paginated notification feed",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cl, err := newClient(cmd)
			if err != nil {
				writeErr(err)
				return nil
			}
			ctx, cancel := ctxWithSignal()
			defer cancel()

			opts := brainapi.ListMessagesOptions{Type: typ, Order: order, Limit: limit, Offset: offset}
			if !all {
				page, err := cl.Messages(ctx, opts)
				if err != nil {
					writeErr(err)
					return nil
				}
				writeOK(page)
				return nil
			}

			out, errs := cl.MessagesAll(ctx, opts)
			var msgs []brainapi.Message
			for {
				select {
				case m, ok := <-out:
					if !ok {
						out = nil
					} else {
						msgs = append(msgs, m)
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
			writeOK(map[string]any{"count": len(msgs), "results": msgs})
			return nil
		},
	}
	cmd.Flags().StringVar(&typ, "type", "", "Filter by message type, e.g. ANNOUNCEMENT (empty = all)")
	cmd.Flags().StringVar(&order, "order", "-dateCreated", "Sort key, e.g. -dateCreated")
	cmd.Flags().IntVar(&limit, "limit", 50, "Page size")
	cmd.Flags().IntVar(&offset, "offset", 0, "Pagination offset")
	cmd.Flags().BoolVar(&all, "all", false, "Drain all pages (default: first page only)")
	return cmd
}
