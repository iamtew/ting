package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/iamtew/ting/internal/control"
	"github.com/iamtew/ting/internal/gateway"
)

func main() {
	os.Exit(run())
}

func run() int {
	listen := flag.String("listen", "", "loopback control listen addr")
	token := flag.String("token", "", "control bearer token")
	specPath := flag.String("spec", "", "JSON gateway spec path")
	flag.Parse()

	if *listen == "" || *token == "" || *specPath == "" {
		fmt.Fprintf(os.Stderr, "ting-connector: -listen, -token, and -spec are required\n")
		return 1
	}

	raw, err := os.ReadFile(*specPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ting-connector: spec: %v\n", err)
		return 1
	}
	var spec gateway.Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		fmt.Fprintf(os.Stderr, "ting-connector: spec json: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := log.New(os.Stderr, "ting-connector ", log.LstdFlags)
	gw := gateway.New(spec, logger)
	ctl := control.New(gw, *token, stop, logger)
	ln, err := control.Listen(*listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ting-connector: control listen: %v\n", err)
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

	logger.Printf("control %s — connecting %s as %s", *listen, spec.Addr(), spec.Identity.Nick)
	if err := gw.Run(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "ting-connector: %v\n", err)
		return 1
	}
	return 0
}
