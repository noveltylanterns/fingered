package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"fingered/internal/config"
	"fingered/internal/server"
)

func main() {
	configPath := flag.String("config", "/etc/fingered/fingered.conf", "path to fingered config")
	internalCGIHelper := flag.Bool("internal-cgi-helper", false, "internal cgi exec helper")
	internalCGIRoot := flag.String("internal-cgi-root", "", "internal cgi chroot")
	internalCGIArgv0 := flag.String("internal-cgi-argv0", "", "internal cgi argv0")
	flag.Parse()

	if *internalCGIHelper {
		if err := server.ExecCGIHelper(*internalCGIRoot, *internalCGIArgv0, 3); err != nil {
			fmt.Fprintf(os.Stderr, "fingered: cgi helper: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fingered: load config: %v\n", err)
		os.Exit(1)
	}

	srv, err := server.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fingered: init server: %v\n", err)
		os.Exit(1)
	}
	defer srv.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := srv.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "fingered: %v\n", err)
		os.Exit(1)
	}
}
