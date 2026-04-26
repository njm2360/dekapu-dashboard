//go:build windows

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
	"gopkg.in/lumberjack.v2"
)

const serviceName = "dkplogparser"

type windowsService struct{}

func (ws *windowsService) Execute(
	args []string,
	r <-chan svc.ChangeRequest,
	s chan<- svc.Status,
) (svcSpecificEC bool, errno uint32) {
	s <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		dir, err := appDir()
		if err != nil {
			done <- err
			return
		}
		done <- runApp(ctx, filepath.Join(dir, "data"))
	}()

	s <- svc.Status{
		State:   svc.Running,
		Accepts: svc.AcceptStop | svc.AcceptShutdown,
	}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Stop, svc.Shutdown:
				s <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				return false, 0
			case svc.Interrogate:
				s <- c.CurrentStatus
			}
		case err := <-done:
			if err != nil {
				log.Printf("watcher exited with error: %v", err)
			}
			return false, 0
		}
	}
}

func installService() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot resolve executable path: %w", err)
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("cannot connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %q already exists", serviceName)
	}

	s, err = m.CreateService(serviceName, exePath, mgr.Config{
		DisplayName: "DKP Log Parser",
		StartType:   mgr.StartAutomatic,
		ServiceType: 0x00000050, // SERVICE_USER_OWN_PROCESS
	})
	if err != nil {
		return fmt.Errorf("cannot create service: %w", err)
	}
	defer s.Close()

	fmt.Printf("Service %q installed (user service, starts on login).\n", serviceName)
	return nil
}

func uninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("cannot connect to SCM: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %q not found: %w", serviceName, err)
	}
	defer s.Close()

	// Stop the service if it is running.
	status, err := s.Query()
	if err == nil && status.State != svc.Stopped {
		if _, err := s.Control(svc.Stop); err != nil {
			return fmt.Errorf("cannot stop service: %w", err)
		}
		// Wait up to 10 s for the service to stop.
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(500 * time.Millisecond)
			status, err = s.Query()
			if err != nil || status.State == svc.Stopped {
				break
			}
		}
	}

	if err := s.Delete(); err != nil {
		return fmt.Errorf("cannot delete service: %w", err)
	}

	fmt.Printf("Service %q uninstalled.\n", serviceName)
	return nil
}

func runAsService() {
	dir, err := appDir()
	if err != nil {
		log.Fatalf("appDir: %v", err)
	}
	_ = godotenv.Load(filepath.Join(dir, ".env"))
	log.SetOutput(&lumberjack.Logger{
		Filename:   filepath.Join(dir, "logs", "watcher.log"),
		MaxSize:    10,
		MaxBackups: 5,
		Compress:   true,
	})

	if err := svc.Run(serviceName, &windowsService{}); err != nil {
		log.Fatalf("service failed: %v", err)
	}
}
