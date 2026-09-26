package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/iamtew/tng/internal/config"
	"github.com/iamtew/tng/internal/master"
)

func main() {
	os.Exit(run())
}

func run() int {
	path := flag.String("config", config.DefaultPath, "TOML config path")
	flag.Parse()

	cfg, err := config.Load(*path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tng-master: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := log.New(os.Stderr, "tng-master ", log.LstdFlags)
	m := master.New(cfg, stop, logger)
	ln, err := master.Listen(cfg.Admin.Listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tng-master: listen: %v\n", err)
		return 1
	}
	go m.Consume(ctx)
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	logger.Printf("admin http://%s (gateway control %s)", cfg.Admin.Listen, cfg.Control.Listen)
	if err := m.Serve(ln); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "tng-master: %v\n", err)
		return 1
	}
	return 0
}
