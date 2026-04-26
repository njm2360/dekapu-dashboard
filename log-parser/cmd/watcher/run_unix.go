//go:build !windows

package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/joho/godotenv"
)

func dataDir() string {
	if v := os.Getenv("DATA_DIR"); v != "" {
		return v
	}
	if base, err := os.UserCacheDir(); err == nil {
		return filepath.Join(base, "dekapu-log-parser", "data")
	}
	return "data"
}

func run() {
	_ = godotenv.Load()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if err := runApp(ctx, dataDir()); err != nil {
		log.Fatalf("watcher error: %v", err)
	}
}
