//go:build windows

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/joho/godotenv"
	"golang.org/x/sys/windows/svc"
)

const appName = "dekapu-log-parser"

func appDir() (string, error) {
	if cache, err := os.UserCacheDir(); err == nil {
		return filepath.Join(cache, appName), nil
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe), nil
	}
	return "", fmt.Errorf("cannot determine application directory")
}

func run() {
	install := flag.Bool("install", false, "Register as a Windows service and exit")
	uninstall := flag.Bool("uninstall", false, "Unregister the Windows service and exit")
	flag.Parse()

	if *install && *uninstall {
		fmt.Fprintln(os.Stderr, "--install and --uninstall cannot be used together")
		os.Exit(1)
	}

	if *install {
		if err := installService(); err != nil {
			log.Fatalf("install failed: %v", err)
		}
		return
	}

	if *uninstall {
		if err := uninstallService(); err != nil {
			log.Fatalf("uninstall failed: %v", err)
		}
		return
	}

	isService, err := svc.IsWindowsService()
	if err != nil {
		log.Fatalf("failed to determine service mode: %v", err)
	}

	if isService {
		runAsService()
		return
	}

	// Console / interactive mode.
	_ = godotenv.Load()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	dir, err := appDir()
	if err != nil {
		log.Fatalf("appDir: %v", err)
	}
	if err := runApp(ctx, filepath.Join(dir, "data")); err != nil {
		log.Fatalf("watcher error: %v", err)
	}
}
