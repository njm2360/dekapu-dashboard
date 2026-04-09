package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"log-parser/internal/autosave"
	"log-parser/internal/cloudsave"
	"log-parser/internal/envutil"
	"log-parser/internal/handler"
	"log-parser/internal/influx"
	"log-parser/internal/offset"
	"log-parser/internal/watcher"
)

func main() {
	_ = godotenv.Load()

	logDir := envutil.Default("VRCHAT_LOG_DIR", "/app/vrchat_log")
	dataDir := "data"
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("failed to create data dir: %v", err)
	}

	influxWriter := influx.NewWriter(
		envutil.Require("INFLUXDB_URL"),
		envutil.Require("INFLUXDB_TOKEN"),
		envutil.Require("INFLUXDB_ORG"),
		envutil.Require("INFLUXDB_BUCKET"),
	)
	defer influxWriter.Close()

	cloudRepo := cloudsave.NewJSONRepository(filepath.Join(dataDir, "cloudsave.json"))
	queue := autosave.NewSaveDispatcher(autosave.NewHTTPSender(&http.Client{Timeout: 15 * time.Second}))
	autosaveInterval := 1800
	if v := os.Getenv("AUTOSAVE_INTERVAL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			autosaveInterval = n
		}
	}
	autoSaveMgr := autosave.NewManager(cloudRepo, time.Duration(autosaveInterval)*time.Second, queue)
	defer autoSaveMgr.Close()

	enableAutosave := envutil.Bool("ENABLE_AUTOSAVE", true)

	newHandler := func(path string) watcher.LineHandler {
		return handler.NewHandler(filepath.Base(path), influxWriter, autoSaveMgr, enableAutosave)
	}

	offsetRepo := offset.NewJSONRepository(filepath.Join(dataDir, "offsets.json"))
	w := watcher.NewLogWatcher(logDir, newHandler, offsetRepo, true)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	log.Printf("Starting watcher. Log dir: %s", logDir)
	if err := w.Run(ctx); err != nil {
		log.Fatalf("watcher error: %v", err)
	}
}
