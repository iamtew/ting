package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/iamtew/ting/internal/config"
	"github.com/iamtew/ting/internal/master"
	"github.com/iamtew/ting/internal/store"
)

func main() {
	os.Exit(run())
}

func run() int {
	path := flag.String("config", config.DefaultPath, "TOML config path")
	connector := flag.String("connector", "", "path to ting-connector binary")
	flag.Parse()

	cfg, err := config.Load(*path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ting-master: %v\n", err)
		return 1
	}
	if *connector != "" {
		cfg.Connector = *connector
	}
	bin := cfg.Connector
	if bin == "" {
		bin = defaultConnector()
	}

	db, err := store.Open(cfg.Database)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ting-master: db: %v\n", err)
		return 1
	}
	defer db.Close()
	if err := db.ImportLegacy(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "ting-master: import: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := log.New(os.Stderr, "ting-master ", log.LstdFlags)
	m := master.New(cfg, db, bin, stop, logger)
	ln, err := master.Listen(cfg.Admin.Listen)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ting-master: listen: %v\n", err)
		return 1
	}
	m.Boot(ctx)
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	logger.Printf("admin http://%s db %s connector %s", cfg.Admin.Listen, cfg.Database, bin)
	if err := m.Serve(ln); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "ting-master: %v\n", err)
		return 1
	}
	return 0
}

func defaultConnector() string {
	exe, err := os.Executable()
	if err != nil {
		return "ting-connector"
	}
	p := filepath.Join(filepath.Dir(exe), "ting-connector")
	if runtime.GOOS == "windows" {
		p += ".exe"
	}
	return p
}
