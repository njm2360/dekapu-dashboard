package autosave

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/url"
)

type Sender interface {
	Send(ctx context.Context, rawURL, userID string) RequestResult
}

type HTTPSender struct {
	client *http.Client
}

func NewHTTPSender(client *http.Client) *HTTPSender {
	return &HTTPSender{client: client}
}

func (s *HTTPSender) Send(ctx context.Context, rawURL, userID string) RequestResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		log.Printf("[AutoSave] Failed to build request user=%s: %v", userID, err)
		return ResultPermanent
	}
	req.Header.Set("User-Agent", "dekapu-dashboard/"+url.PathEscape(userID))

	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("[AutoSave] Connection failed user=%s: %v", userID, err)
		return ResultRetryable
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1000))
	if err != nil {
		body = []byte("<unreadable>")
	}

	switch {
	case resp.StatusCode == 200:
		log.Printf("[AutoSave] Success user=%s", userID)
		return ResultOK
	case resp.StatusCode >= 500:
		log.Printf("[AutoSave] Server error %d user=%s — %s", resp.StatusCode, userID, body)
		return ResultRetryable
	case resp.StatusCode >= 400:
		log.Printf("[AutoSave] Client error %d user=%s — %s", resp.StatusCode, userID, body)
		return ResultPermanent
	default:
		log.Printf("[AutoSave] Unhandled status %d user=%s", resp.StatusCode, userID)
		return ResultRetryable
	}
}
