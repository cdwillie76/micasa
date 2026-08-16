// Copyright 2026 Phillip Cloud
// Licensed under the Apache License, Version 2.0

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/micasa-dev/micasa/internal/config"
	"github.com/micasa-dev/micasa/internal/data"
	"github.com/micasa-dev/micasa/internal/webui"
	"github.com/spf13/cobra"
)

// defaultWebAddr binds to loopback only -- reachable from a phone or other
// LAN device requires explicitly passing --addr, not a surprise default.
const defaultWebAddr = "127.0.0.1:8734"

// webOpts holds flags for the web subcommand.
type webOpts struct {
	dbPath string
	addr   string
	open   bool
}

func newWebCmd() *cobra.Command {
	opts := &webOpts{}

	cmd := &cobra.Command{
		Use:   "web [database-path]",
		Short: "Launch a local browser-based UI",
		Long: "Launch a local HTTP server serving a browser-based UI against the " +
			"same database the TUI uses. Binds to localhost by default.",
		Args:          cobra.MaximumNArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				opts.dbPath = args[0]
			}
			return runWeb(cmd.OutOrStdout(), opts)
		},
	}

	cmd.Flags().StringVar(&opts.addr, "addr", defaultWebAddr, "Address to listen on")
	cmd.Flags().BoolVar(&opts.open, "open", true, "Open the default browser after starting")

	return cmd
}

// resolveDBPath returns the database path to use, mirroring runOpts.resolveDBPath.
func (opts *webOpts) resolveDBPath() (string, error) {
	if opts.dbPath != "" {
		return data.ExpandHome(opts.dbPath), nil
	}
	return data.DefaultDBPath()
}

func runWeb(w io.Writer, opts *webOpts) error {
	dbPath, err := opts.resolveDBPath()
	if err != nil {
		return fmt.Errorf("resolve db path: %w", err)
	}

	store, err := data.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = store.Close() }()

	if err := store.AutoMigrate(); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	if err := store.SeedDefaults(); err != nil {
		return fmt.Errorf("seed defaults: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := store.ResolveCurrency(cfg.Locale.Currency); err != nil {
		return fmt.Errorf("resolve currency: %w", err)
	}
	units, err := store.GetUnitSystem()
	if err != nil {
		return fmt.Errorf("resolve unit system: %w", err)
	}
	if err := store.SetMaxDocumentSize(cfg.Documents.MaxFileSize.Bytes()); err != nil {
		return fmt.Errorf("configure document size limit: %w", err)
	}

	srv, err := webui.NewServer(store, store.Currency(), units)
	if err != nil {
		return fmt.Errorf("initialize web server: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	url := "http://" + opts.addr
	_, _ = fmt.Fprintf(w, "micasa web listening on %s (Ctrl+C to stop)\n", url)
	if opts.open {
		if err := openBrowser(ctx, url); err != nil {
			_, _ = fmt.Fprintf(w, "warning: could not open browser automatically: %v\n", err)
		}
	}

	return srv.Run(ctx, opts.addr)
}

// openBrowser best-effort launches the platform's default browser at url.
// Failures are non-fatal -- the server keeps running regardless.
func openBrowser(ctx context.Context, url string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{url}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		name, args = "xdg-open", []string{url}
	}
	if err := exec.CommandContext( //nolint:gosec // url is our own loopback address, not user input
		ctx, name, args...,
	).Start(); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	return nil
}
