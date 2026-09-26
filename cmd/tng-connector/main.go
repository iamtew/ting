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
	"github.com/iamtew/tng/internal/control"
	"github.com/iamtew/tng/internal/gateway"
)

func main() {
	os.Exit(run())
}

func run() int {
	path := flag.String("config", config.DefaultPath, "TOML config path")
	flag.Parse()

	cfg, err := config.Load(*path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tng-connector: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := log.New(os.Stderr, "tng-connector ", log.LstdFlags)
	gw := gateway.New(cfg, logger)
	ctl := control.New(gw, cfg.Control.Token, stop, logger)
	ln, err := control.Listen(cfg.Control.Listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tng-connector: control listen: %v\n", err)
		return 1
	}
	go func() {
		if err := ctl.Serve(ln); err != nil {
			logger.Printf("control: %v", err)
		}
	}()
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	logger.Printf("control %s — connecting %s as %s", cfg.Control.Listen, cfg.Addr(), cfg.Identity.Nick)
	if err := gw.Run(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "tng-connector: %v\n", err)
		return 1
	}
	return 0
}
