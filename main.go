//go:build linux

package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/robfig/cron/v3"

	"github.com/mcreekmore/cross-seed-cleanup/internal/cleanup"
	"github.com/mcreekmore/cross-seed-cleanup/internal/env"
)

func main() {
	cleanup.SetupLogging()

	if len(os.Args) > 1 && os.Args[1] == "run" {
		slog.Info("running cross-seed-cleanup")
		cleanup.Run()
		return
	}

	schedule := env.Getenv("SCHEDULE", "")
	if schedule == "" {
		cleanup.Run()
		return
	}

	runOnStart := env.GetenvBool("RUN_ON_START", true)

	slog.Info("scheduler configured", "schedule", schedule, "run_on_start", runOnStart)
	if runOnStart {
		slog.Info("executing initial run")
		cleanup.Run()
	}

	c := cron.New()
	_, err := c.AddFunc(schedule, func() {
		slog.Info("scheduled run starting")
		cleanup.Run()
		slog.Info("scheduled run complete")
	})
	if err != nil {
		slog.Error("invalid SCHEDULE expression", "schedule", schedule, "err", err)
		os.Exit(1)
	}

	c.Start()
	slog.Info("cron scheduler started, waiting for next run")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	slog.Info("shutting down scheduler")
	c.Stop()
}
