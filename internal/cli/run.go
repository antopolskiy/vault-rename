package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/antopolskiy/vault-rename/internal/app"
	"github.com/antopolskiy/vault-rename/internal/apperr"
	"github.com/antopolskiy/vault-rename/internal/model"
)

var Version = "0.0.0-dev"

type errorOutput struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func Run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("vault-rename", flag.ContinueOnError)
	flags.SetOutput(stderr)

	root := flags.String("root", ".", "Obsidian vault root")
	configPath := flags.String("config", "", "Configuration path (default: <root>/.vault-rename.toml)")
	dryRun := flags.Bool("dry-run", false, "Validate and print the operation without writing")
	jsonOutput := flags.Bool("json", false, "Print machine-readable output")
	reason := flags.String("reason", "", "Audit reason (required for a mutating rename)")
	actor := flags.String("actor", "", "Audit actor (default: current OS user)")
	batchID := flags.String("batch-id", "", "Optional batch identifier")
	backlinks := flags.String("backlinks", "", "Backlink mode override: repair, check, or off")
	showVersion := flags.Bool("version", false, "Print version")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: vault-rename [options] <source> <new-name>")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, Version)
		return 0
	}
	if flags.NArg() != 2 {
		flags.Usage()
		return 2
	}

	result, err := app.Run(context.Background(), model.Request{
		Root:              *root,
		ConfigPath:        *configPath,
		Source:            flags.Arg(0),
		NewName:           flags.Arg(1),
		Reason:            *reason,
		Actor:             *actor,
		BatchID:           *batchID,
		DryRun:            *dryRun,
		BacklinksOverride: model.BacklinkMode(*backlinks),
	}, Version)
	if err != nil {
		return renderError(err, *jsonOutput, stdout, stderr)
	}

	if *jsonOutput {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "vault-rename: write output: %v\n", err)
			return 1
		}
		return 0
	}
	verb := "Would rename"
	if result.Status == "committed" {
		verb = "Renamed"
	}
	fmt.Fprintf(stdout, "%s: %s -> %s\n", verb, result.Source, result.Destination)
	fmt.Fprintf(stdout, "Files changed: %d\n", result.FilesChanged)
	fmt.Fprintf(stdout, "Links updated: %d\n", result.LinksUpdated)
	if result.OperationID != "" {
		fmt.Fprintf(stdout, "Operation id: %s\n", result.OperationID)
	}
	if result.LogPath != "" {
		fmt.Fprintf(stdout, "Ledger: %s\n", result.LogPath)
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(stdout, "Warning: %s", warning.Message)
		if warning.Path != "" {
			fmt.Fprintf(stdout, " (%s)", warning.Path)
		}
		fmt.Fprintln(stdout)
	}
	return 0
}

func renderError(err error, jsonOutput bool, stdout, stderr io.Writer) int {
	var appError *apperr.Error
	if !errors.As(err, &appError) {
		appError = apperr.Wrap(apperr.CodeIOError, "unexpected error", err)
	}
	if jsonOutput {
		if writeErr := writeJSON(stdout, errorOutput{
			Code: appError.Code, Message: appError.Message, Details: appError.Details,
		}); writeErr != nil {
			fmt.Fprintf(stderr, "vault-rename: write error output: %v\n", writeErr)
		}
	} else {
		fmt.Fprintf(stderr, "vault-rename: %s: %s\n", appError.Code, appError.Message)
	}
	if appError.Code == apperr.CodeConfigError {
		return 2
	}
	return 1
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
