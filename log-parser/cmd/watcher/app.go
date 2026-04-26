package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"log-parser/internal/autosave"
	"log-parser/internal/cloudsave"
	"log-parser/internal/envutil"
	"log-parser/internal/handler"
	"log-parser/internal/influx"
	"log-parser/internal/offset"
	"log-parser/internal/watcher"
)

func runApp(ctx context.Context, dataDir string) error {
	logDir, err := envutil.Get("VRCHAT_LOG_DIR")
	if err != nil {
		return err
	}
	influxURL, err := envutil.Get("INFLUXDB_URL")
	if err != nil {
		return err
	}
	influxToken, err := envutil.Get("INFLUXDB_TOKEN")
	if err != nil {
		return err
	}
	influxOrg, err := envutil.Get("INFLUXDB_ORG")
	if err != nil {
		return err
	}
	influxBucket, err := envutil.Get("INFLUXDB_BUCKET")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("failed to create data dir: %w", err)
	}

	influxWriter := influx.NewWriter(influxURL, influxToken, influxOrg, influxBucket)
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

	log.Printf("Starting watcher. Log dir: %s", logDir)
	return w.Run(ctx)
}
