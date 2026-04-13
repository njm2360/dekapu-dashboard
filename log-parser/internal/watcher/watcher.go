package watcher

import (
	"bufio"
	"context"
	"io"
	"log"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultStateSaveInterval = 60 * time.Second
	defaultPollInterval      = 1 * time.Second
	defaultScanInterval      = 30 * time.Second
	defaultIdleTimeout       = 1800 * time.Second
	logPattern               = "output_log_*.txt"
)

type LineHandler func(path, line string)

type OffsetRepository interface {
	Load() (map[string]int64, error)
	Save(offsets map[string]int64) error
}

type LogWatcher struct {
	logDir      string
	newHandler  func(path string) LineHandler
	offsetRepo  OffsetRepository
	readFromEnd bool

	pollInterval      time.Duration
	scanInterval      time.Duration
	stateSaveInterval time.Duration
	idleTimeout       time.Duration

	mu         sync.Mutex
	offsets    map[string]int64
	knownFiles map[string]bool
	cancelFns  map[string]context.CancelFunc
	handlers   map[string]LineHandler

	fileWg sync.WaitGroup
}

func (w *LogWatcher) WithIntervals(poll, scan, stateSave, idle time.Duration) *LogWatcher {
	w.pollInterval = poll
	w.scanInterval = scan
	w.stateSaveInterval = stateSave
	w.idleTimeout = idle
	return w
}

func NewLogWatcher(logDir string, newHandler func(path string) LineHandler, offsetRepo OffsetRepository, readFromEnd bool) *LogWatcher {
	absLogDir, err := filepath.Abs(logDir)
	if err != nil {
		log.Printf("Failed to resolve logDir to absolute path: %v", err)
	} else {
		logDir = absLogDir
	}

	offsets, err := offsetRepo.Load()
	if err != nil {
		log.Printf("Failed to load offsets: %v", err)
		offsets = make(map[string]int64)
	}

	for path := range offsets {
		if !filepath.IsAbs(path) {
			abs := filepath.Join(logDir, filepath.Base(path))
			log.Printf("Migrating relative offset key: %s -> %s", path, abs)
			offsets[abs] = offsets[path]
			delete(offsets, path)
			path = abs
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			log.Printf("Removed stale state entry: %s", path)
			delete(offsets, path)
		}
	}

	return &LogWatcher{
		logDir:            logDir,
		newHandler:        newHandler,
		offsetRepo:        offsetRepo,
		readFromEnd:       readFromEnd,
		pollInterval:      defaultPollInterval,
		scanInterval:      defaultScanInterval,
		stateSaveInterval: defaultStateSaveInterval,
		idleTimeout:       defaultIdleTimeout,
		offsets:           offsets,
		knownFiles:        make(map[string]bool),
		cancelFns:         make(map[string]context.CancelFunc),
		handlers:          make(map[string]LineHandler),
	}
}

func (w *LogWatcher) Run(ctx context.Context) error {
	log.Printf("LogWatcher starting (dir=%s)", w.logDir)
	var wg sync.WaitGroup

	wg.Go(func() { w.scanLoop(ctx) })
	wg.Go(func() { w.stateSaveLoop(ctx) })

	<-ctx.Done()
	log.Printf("LogWatcher shutting down...")

	w.mu.Lock()
	for _, cancel := range w.cancelFns {
		cancel()
	}
	w.mu.Unlock()

	wg.Wait()
	w.fileWg.Wait()
	log.Printf("All file watchers stopped.")

	w.mu.Lock()
	offsets := maps.Clone(w.offsets)
	w.mu.Unlock()
	if err := w.offsetRepo.Save(offsets); err != nil {
		log.Printf("Failed to save offsets: %v", err)
	} else {
		log.Printf("Offsets saved.")
	}

	return nil
}

func (w *LogWatcher) startWatchFile(ctx context.Context, path string) {
	w.mu.Lock()
	if _, already := w.cancelFns[path]; already {
		w.mu.Unlock()
		return
	}
	fileCtx, cancel := context.WithCancel(ctx)
	w.knownFiles[path] = true
	w.cancelFns[path] = cancel
	handler := w.newHandler(path)
	w.handlers[path] = handler
	w.mu.Unlock()

	w.fileWg.Go(func() {
		defer cancel()
		defer func() {
			w.mu.Lock()
			delete(w.cancelFns, path)
			delete(w.handlers, path)
			w.mu.Unlock()
		}()
		w.watchFile(fileCtx, path, handler)
	})
}

func (w *LogWatcher) watchFile(ctx context.Context, path string, handler LineHandler) {
	f, err := os.Open(path)
	if err != nil {
		log.Printf("watchFile open %s: %v", path, err)
		return
	}
	defer f.Close()

	w.mu.Lock()
	offset, hasOffset := w.offsets[path]
	w.mu.Unlock()

	if hasOffset {
		if offset > 0 {
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				log.Printf("watchFile seek %s: %v", path, err)
				return
			}
		}
	} else if w.readFromEnd {
		endPos, err := f.Seek(0, io.SeekEnd)
		if err != nil {
			log.Printf("watchFile seek-end %s: %v", path, err)
			return
		}
		w.mu.Lock()
		w.offsets[path] = endPos
		w.mu.Unlock()
	}

	jitter := rand.N(w.pollInterval)
	select {
	case <-ctx.Done():
		return
	case <-time.After(jitter):
	}

	lastActive := time.Now()
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		lines, newOffset, err := readLines(f)
		if err != nil {
			log.Printf("watchFile read %s: %v", path, err)
			return
		}

		if len(lines) > 0 {
			lastActive = time.Now()
			w.mu.Lock()
			w.offsets[path] = newOffset
			w.mu.Unlock()
			for _, line := range lines {
				handler(path, line)
			}
		} else if time.Since(lastActive) >= w.idleTimeout {
			log.Printf("File is stale. Remove from monitoring task: %s", path)
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *LogWatcher) scanLoop(ctx context.Context) {
	ticker := time.NewTicker(w.scanInterval)
	defer ticker.Stop()

	w.doScan(ctx, true)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.doScan(ctx, false)
		}
	}
}

func (w *LogWatcher) doScan(ctx context.Context, firstScan bool) {
	matches, err := filepath.Glob(filepath.Join(w.logDir, logPattern))
	if err != nil {
		log.Printf("scan glob: %v", err)
		return
	}

	w.mu.Lock()
	knownSnap := maps.Clone(w.knownFiles)
	offsetSnap := maps.Clone(w.offsets)
	activeSnap := make(map[string]bool, len(w.cancelFns))
	for k := range w.cancelFns {
		activeSnap[k] = true
	}
	w.mu.Unlock()

	for _, path := range matches {
		if !knownSnap[path] {
			if !firstScan {
				if _, exists := offsetSnap[path]; !exists {
					w.mu.Lock()
					w.offsets[path] = 0
					w.mu.Unlock()
					offsetSnap[path] = 0
				}
			}
			offset := offsetSnap[path]
			if offset > 0 {
				info, statErr := os.Stat(path)
				if statErr != nil {
					continue
				}
				if info.Size() <= offset {
					w.mu.Lock()
					w.knownFiles[path] = true
					w.mu.Unlock()
					continue
				}
			}
			w.startWatchFile(ctx, path)
			log.Printf("Monitoring start: %s", path)
			continue
		}

		if !activeSnap[path] {
			info, statErr := os.Stat(path)
			if statErr != nil {
				w.mu.Lock()
				delete(w.knownFiles, path)
				w.mu.Unlock()
				continue
			}
			if info.Size() > offsetSnap[path] {
				w.startWatchFile(ctx, path)
				log.Printf("Monitoring resume: %s", path)
			}
		}
	}
}

func (w *LogWatcher) stateSaveLoop(ctx context.Context) {
	ticker := time.NewTicker(w.stateSaveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.mu.Lock()
			offsets := maps.Clone(w.offsets)
			w.mu.Unlock()
			if err := w.offsetRepo.Save(offsets); err != nil {
				log.Printf("Failed to save offsets: %v", err)
			}
		}
	}
}

func readLines(f *os.File) (lines []string, newOffset int64, err error) {
	startPos, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, 0, err
	}

	reader := bufio.NewReader(f)
	pos := startPos

	for {
		raw, readErr := reader.ReadString('\n')
		if strings.HasSuffix(raw, "\n") {
			pos += int64(len(raw))
			lines = append(lines, strings.TrimRight(raw, "\r\n"))
		}
		if readErr != nil {
			break
		}
	}

	if _, err := f.Seek(pos, io.SeekStart); err != nil {
		return lines, pos, err
	}
	return lines, pos, nil
}
