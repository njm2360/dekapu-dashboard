package autosave

import (
	"context"
	"log"
	"sync"
	"time"
)

const (
	workerBufSize = 16
	maxRetries    = 5
	baseBackoff   = 2 * time.Second
	maxBackoff    = 2 * time.Minute
)

type saveJob struct {
	rawURL string
	userID string
}

type userWorker struct {
	ch chan saveJob
}

type RequestResult int

const (
	ResultOK        RequestResult = iota
	ResultRetryable               // network errors, 5xx
	ResultPermanent               // malformed URL, 4xx
)

type Dispatcher interface {
	Enqueue(userID, rawURL string) bool
	Close()
}

type SaveDispatcher struct {
	sender      Sender
	done        chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	backoffBase time.Duration
	backoffMax  time.Duration

	mu      sync.Mutex
	workers map[string]*userWorker
	wg      sync.WaitGroup
}

func NewSaveDispatcher(sender Sender) *SaveDispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &SaveDispatcher{
		sender:      sender,
		done:        make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
		workers:     make(map[string]*userWorker),
		backoffBase: baseBackoff,
		backoffMax:  maxBackoff,
	}
}

func (q *SaveDispatcher) Enqueue(userID, rawURL string) bool {
	select {
	case <-q.done:
		log.Printf("[AutoSave] SaveDispatcher is closed, dropping job for user=%s", userID)
		return false
	default:
	}
	j := saveJob{rawURL: rawURL, userID: userID}
	w := q.getOrCreateWorker(userID)
	select {
	case <-q.done:
		log.Printf("[AutoSave] SaveDispatcher is closed, dropping job for user=%s", userID)
		return false
	case w.ch <- j:
		return true
	default:
		log.Printf("[AutoSave] Buffer full, dropping job for user=%s", userID)
		return false
	}
}

func (q *SaveDispatcher) Close() {
	q.mu.Lock()
	var pending int
	for _, w := range q.workers {
		pending += len(w.ch)
	}
	q.mu.Unlock()

	if pending > 0 {
		log.Printf("[AutoSave] Discarding %d pending request(s) on shutdown", pending)
	}

	q.cancel()
	close(q.done)
	q.wg.Wait()
}

func (q *SaveDispatcher) getOrCreateWorker(userID string) *userWorker {
	q.mu.Lock()
	defer q.mu.Unlock()

	if w, ok := q.workers[userID]; ok {
		return w
	}

	w := &userWorker{ch: make(chan saveJob, workerBufSize)}
	q.workers[userID] = w
	q.wg.Add(1)
	go q.runWorker(w)
	return w
}

func (q *SaveDispatcher) runWorker(w *userWorker) {
	defer q.wg.Done()
	for {
		select {
		case j := <-w.ch:
			q.doRequestWithBackoff(j)
		case <-q.done:
			return
		}
	}
}

func (q *SaveDispatcher) doRequestWithBackoff(j saveJob) {
	backoff := q.backoffBase
	for attempt := range maxRetries + 1 {
		if attempt > 0 {
			select {
			case <-q.done:
				log.Printf("[AutoSave] Retry cancelled (shutdown) user=%s attempt=%d", j.userID, attempt)
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, q.backoffMax)
		}

		switch q.sender.Send(q.ctx, j.rawURL, j.userID) {
		case ResultOK, ResultPermanent:
			return
		case ResultRetryable:
			if attempt < maxRetries {
				log.Printf("[AutoSave] Will retry (attempt %d/%d, backoff %s) user=%s", attempt+1, maxRetries, backoff, j.userID)
			} else {
				log.Printf("[AutoSave] Gave up after %d attempts user=%s", maxRetries+1, j.userID)
			}
		}
	}
}
