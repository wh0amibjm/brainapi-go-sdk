// Package jsonout writes the CLI's stable {ok,data,error} JSON envelope to stdout.
// Library callers should NOT depend on this — it exists only to keep the CLI's
// interface uniform across every subcommand.
package jsonout

import (
	"encoding/json"
	"io"
	"os"
)

// Envelope is the on-the-wire shape. Exactly one of Data / Error is set.
type Envelope struct {
	OK    bool      `json:"ok"`
	Data  any       `json:"data,omitempty"`
	Error *ErrorBox `json:"error,omitempty"`
}

// ErrorBox is the structured error returned by the CLI.
type ErrorBox struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// Success writes {"ok":true,"data":...} to stdout.
func Success(data any) error {
	return write(os.Stdout, Envelope{OK: true, Data: data})
}

// Failure writes {"ok":false,"error":{...}} to stdout.
func Failure(kind, msg string, details any) error {
	return write(os.Stdout, Envelope{OK: false, Error: &ErrorBox{Kind: kind, Message: msg, Details: details}})
}

func write(w io.Writer, env Envelope) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}
