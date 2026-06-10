package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/acronis/go-raml/v3"
	"github.com/acronis/go-stacktrace"
	"github.com/acronis/go-stacktrace/slogex"
)

type ValidateOptions struct {
	EnsureDuplicates bool
	AllowRemote      bool
	WorkspaceRoot    string
	NoWorkspaceGuard bool
}

type ValidateCommand struct {
	Opts ValidateOptions
	Args []string
}

func NewValidateCmd(opts ValidateOptions, args []string) *ValidateCommand {
	return &ValidateCommand{
		Opts: opts,
		Args: args,
	}
}

func (v ValidateCommand) Execute(ctx context.Context) error {
	var err error
	var stOpts []stacktrace.TracesOpt
	if v.Opts.EnsureDuplicates {
		stOpts = append(stOpts, stacktrace.WithEnsureDuplicates())
	}
	for _, arg := range v.Args {
		slog.Info("Validating RAML...", slog.String("path", arg))
		parseOpts := []raml.ParseOpt{raml.OptWithUnwrap(), raml.OptWithValidate()}
		switch {
		case v.Opts.NoWorkspaceGuard:
			parseOpts = append(parseOpts, raml.OptWithFileLoader(raml.OSFileLoader{}))
		case v.Opts.WorkspaceRoot != "":
			parseOpts = append(parseOpts, raml.OptWithWorkspaceRoot(v.Opts.WorkspaceRoot))
		}
		if v.Opts.AllowRemote {
			parseOpts = append(parseOpts, raml.OptWithHTTPClient(raml.NewHTTPClient()))
		}
		_, err = raml.ParseFromPathCtx(ctx, arg, parseOpts...)
		if err != nil {
			slog.Error("RAML is invalid", slogex.ErrToSlogAttr(err, stOpts...))
		} else {
			slog.Info("RAML is valid", slog.String("path", arg))
		}
	}
	if err != nil {
		return fmt.Errorf("errors have been found in the RAML files")
	}
	return nil
}
