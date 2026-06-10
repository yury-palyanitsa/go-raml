package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"github.com/acronis/go-stacktrace"
	"github.com/acronis/go-stacktrace/slogex"
	"github.com/spf13/cobra"
)

type CommandError struct {
	Inner error
	Msg   string
}

func (e *CommandError) Error() string {
	return fmt.Sprintf("%s: %v", e.Msg, e.Inner)
}

func (e *CommandError) Unwrap() error {
	return e.Inner
}

func NewCommandError(err error, msg string) error {
	if err != nil {
		return &CommandError{Inner: err, Msg: msg}
	}
	return nil
}

type Command interface {
	Execute(ctx context.Context) error
}

func InitLoggingAndRun(ctx context.Context, verbosity int, cmd Command) error {
	lvl := slog.LevelInfo
	if verbosity > 0 {
		lvl = slog.LevelDebug
	}
	InitLogging(lvl)
	return NewCommandError(cmd.Execute(ctx), "command error")
}

func main() {
	os.Exit(mainFn())
}

func mainFn() int {
	var ensureDuplicates bool
	verbosity := 0
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer stop()

	cmdValidate := func() *cobra.Command {
		opts := ValidateOptions{
			EnsureDuplicates: ensureDuplicates,
		}
		cmd := &cobra.Command{
			Use:   "validate",
			Short: "validate raml files",
			Args:  cobra.MinimumNArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return InitLoggingAndRun(ctx, verbosity, NewValidateCmd(opts, args))
			},
		}

		cmd.Flags().StringVarP(&opts.WorkspaceRoot, "workspace-root", "w", "",
			"restrict file loading to this directory (absolute path); "+
				"any !include or uses: outside the root is rejected")
		cmd.Flags().BoolVar(&opts.NoWorkspaceGuard, "no-workspace-guard", false,
			"disable the workspace file-loading restriction; allows includes to resolve to any path")
		cmd.Flags().BoolVar(&opts.AllowRemote, "remote", false,
			"enable resolution of HTTP/HTTPS remote fragments (!include https://...)")

		return cmd
	}()

	cmdConvert := func() *cobra.Command {
		opts := ConvertOptions{}
		cmd := &cobra.Command{
			Use:   "convert [flags] <file.raml>...",
			Short: "convert RAML files to another format",
			Long: `Convert one or more API definition files.

Currently supported output formats:
  oas3         OpenAPI Specification 3.0.3 (JSON)  [input: RAML 1.0]
  jsonschema   JSON Schema draft-07 (one file per type, or --type for a single type)  [input: RAML 1.0]
  raml         RAML 1.0 DataType or Library  [input: JSON Schema]

When --output is a file path and multiple inputs are given, --output is treated
as a directory; each result is written as:
  oas3       → <basename>.openapi.json
  jsonschema → <TypeName>.schema.json
  raml       → <basename>.raml`,
			Args: cobra.MinimumNArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return InitLoggingAndRun(ctx, verbosity, NewConvertCmd(opts, args))
			},
		}

		cmd.Flags().StringVarP(&opts.Format, "format", "f", "oas3",
			`output format: "oas3", "jsonschema", "raml" (JSON Schema → RAML 1.0)`)
		cmd.Flags().StringVarP(&opts.Output, "output", "o", "",
			`output file or directory (default: stdout; use "-" for stdout explicitly)`)
		cmd.Flags().StringVarP(&opts.TypeName, "type", "t", "",
			`(jsonschema) convert only the named type instead of all types`)
		cmd.Flags().StringVarP(&opts.WorkspaceRoot, "workspace-root", "w", "",
			"restrict file loading to this directory (absolute path); "+
				"any !include or uses: outside the root is rejected")
		cmd.Flags().BoolVar(&opts.NoWorkspaceGuard, "no-workspace-guard", false,
			"disable the workspace file-loading restriction; allows includes to resolve to any path")
		cmd.Flags().BoolVar(&opts.AllowRemote, "remote", false,
			"enable resolution of HTTP/HTTPS remote fragments (!include https://...)")

		return cmd
	}()

	rootCmd := func() *cobra.Command {
		cmd := &cobra.Command{
			Use:           "raml",
			Short:         "raml is a RAML 1.0 tool",
			SilenceUsage:  true,
			SilenceErrors: true,
			CompletionOptions: cobra.CompletionOptions{
				DisableDefaultCmd: true,
			},
		}

		cmd.PersistentFlags().CountVarP(&verbosity, "verbosity", "v", "increase verbosity level: -v for debug")
		cmd.Flags().BoolVarP(&ensureDuplicates, "ensure-duplicates", "d", false,
			"ensure that there are no duplicates in tracebacks")

		cmd.AddCommand(
			cmdValidate,
			cmdConvert,
		)
		return cmd
	}()

	if err := rootCmd.Execute(); err != nil {
		var cmdErr *CommandError
		if errors.As(err, &cmdErr) && cmdErr.Inner != nil {
			stOpts := []stacktrace.TracesOpt{}
			if ensureDuplicates {
				stOpts = append(stOpts, stacktrace.WithEnsureDuplicates())
			}
			slog.Error("Command failed", slogex.ErrToSlogAttr(cmdErr.Inner, stOpts...))
		} else {
			_ = rootCmd.Usage()
		}
		return 1
	}

	return 0
}
