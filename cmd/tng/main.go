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
		fmt.Fprintf(os.Stderr, "tng: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := log.New(os.Stderr, "", log.LstdFlags)
	gw := gateway.New(cfg, logger)
	go func() {
		for ev := range gw.Events() {
			switch ev.Kind {
			case "line":
				logger.Printf("<- %s", ev.Msg.Encode())
			default:
				logger.Printf("%s", ev.Kind)
			}
		}
	}()

	logger.Printf("connecting %s as %s", cfg.Addr(), cfg.Identity.Nick)
	if err := gw.Run(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "tng: %v\n", err)
		return 1
	}
	return 0
}
