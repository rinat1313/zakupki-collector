package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/rinat1313/zakupki-collector/internal/collector"
	"github.com/rinat1313/zakupki-collector/internal/config"
	"github.com/rinat1313/zakupki-collector/internal/eis"
	"github.com/rinat1313/zakupki-collector/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	client, err := eis.NewClient(eis.ClientOptions{
		Endpoint:   cfg.EISEndpoint,
		Token:      cfg.EISToken,
		Mode:       cfg.EISMode,
		Profile:    cfg.EISProfile,
		TLSCert:    cfg.EISTLSCert,
		TLSKey:     cfg.EISTLSKey,
		CACert:     cfg.EISCACert,
		SkipVerify: cfg.EISSkipVerify,
	})
	if err != nil {
		log.Error("eis client", "err", err)
		os.Exit(1)
	}

	runner := collector.New(cfg, client, st, log)
	if err := runner.Run(ctx); err != nil && ctx.Err() == nil {
		log.Error("runner", "err", err)
		os.Exit(1)
	}
}
