package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/flare-foundation/go-flare-common/pkg/logger"
	"github.com/flare-network/rebalancer/internal/rebalancer"
)

func main() {
	configFile := flag.String("config", "rebalancer.toml", "Path to configuration file")
	flag.Parse()

	logger.Info("Starting rebalancer")

	cfg, err := rebalancer.Load(*configFile)
	if err != nil {
		logger.Errorf("Failed to load config: %v", err)
		os.Exit(1)
	}

	rb, err := rebalancer.New(cfg)
	if err != nil {
		logger.Errorf("Failed to create rebalancer: %v", err)
		os.Exit(1)
	}

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sig := <-signalChan
		logger.Infof("Received %v signal, shutting down", sig)
		cancel()
	}()

	if err := rb.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Errorf("Rebalancer exited with error: %v", err)
		os.Exit(1)
	}

	logger.Info("Stopped rebalancer")
}
