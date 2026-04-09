package parser

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"log-parser/internal/model"
)

type Event int

const (
	EventSavedataUpdate Event = iota
	EventWorldJoin
	EventWorldLeave
	EventSessionReset
	EventCloudLoad
	EventJpStockover
	EventVRChatAppQuit
)

type ParseResult struct {
	Event          Event
	Record         *model.MmpSaveRecord // only set for EventSavedataUpdate
	StockoverValue int64                // only set for EventJpStockover
}

const worldID = "wrld_1af53798-92a3-4c3f-99ae-a7c42ec6084d"

var (
	savedataURLPattern    = regexp.MustCompile(`^https?://[^/]+/api/v(\d+)/data`)
	stockoverValuePattern = regexp.MustCompile(`:\s*([\d,]+)$`)
)

const (
	cloudLoadMsg    = "[LoadFromParsedData]"
	sessionResetMsg = "[ResetCurrentSession]"
	jpStockOverMsg  = "[JP] ストック溢れです"
	worldJoinMsg    = "[Behaviour] Joining " + worldID
	worldLeaveMsg   = "[OnPlayerLeft] ローカルプレイヤーが Leave した"    // Only when normal leave
	appQuitMsg      = "VRCApplication: HandleApplicationQuit" // Only when app quit
)

type MmpLogParser struct {
	fname string
}

func NewMmpLogParser(fname string) *MmpLogParser {
	return &MmpLogParser{fname: fname}
}

func (p *MmpLogParser) ParseLine(line string) *ParseResult {
	switch {
	case strings.Contains(line, cloudLoadMsg):
		return &ParseResult{Event: EventCloudLoad}
	case strings.Contains(line, sessionResetMsg):
		return &ParseResult{Event: EventSessionReset}
	case strings.Contains(line, jpStockOverMsg):
		val, ok := p.parseStockoverValue(line)
		if !ok {
			return nil
		}
		return &ParseResult{Event: EventJpStockover, StockoverValue: val}
	case strings.Contains(line, worldJoinMsg):
		return &ParseResult{Event: EventWorldJoin}
	case strings.Contains(line, worldLeaveMsg):
		return &ParseResult{Event: EventWorldLeave}
	case strings.Contains(line, appQuitMsg):
		return &ParseResult{Event: EventVRChatAppQuit}
	}

	if m := savedataURLPattern.FindStringSubmatch(line); m != nil {
		version, _ := strconv.Atoi(m[1])
		record, err := p.parseSavedataLine(line, version)
		if err != nil {
			log.Printf("[%s] Save data parse error: %v", p.fname, err)
			return nil
		}
		if record == nil {
			return nil
		}
		return &ParseResult{Event: EventSavedataUpdate, Record: record}
	}

	return nil
}

func (p *MmpLogParser) parseStockoverValue(line string) (int64, bool) {
	m := stockoverValuePattern.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	raw := strings.ReplaceAll(m[1], ",", "")
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		log.Printf("[%s] Failed to parse JP stockover '%s': %v", p.fname, raw, err)
		return 0, false
	}
	return v, true
}

func (p *MmpLogParser) parseSavedataLine(line string, version int) (*model.MmpSaveRecord, error) {
	parsed, err := url.Parse(line)
	if err != nil {
		return nil, fmt.Errorf("url parse: %w", err)
	}
	q := parsed.Query()

	getParam := func(name string) (string, bool) {
		v := q.Get(name)
		if v == "" {
			log.Printf("[%s] Missing or empty parameter: %s", p.fname, name)
			return "", false
		}
		return v, true
	}

	rawData, ok := getParam("data")
	if !ok {
		return nil, nil
	}
	rawUserID, ok := getParam("user_id")
	if !ok {
		return nil, nil
	}
	sig, ok := getParam("sig")
	if !ok {
		return nil, nil
	}

	decode := func(s string) (string, error) {
		if version == 4 {
			b, err := b64URLSafeDecode(s)
			if err != nil {
				return "", fmt.Errorf("base64 decode: %w", err)
			}
			return string(b), nil
		}
		return s, nil
	}

	dataStr, err := decode(rawData)
	if err != nil {
		return nil, err
	}
	userID, err := decode(rawUserID)
	if err != nil {
		return nil, err
	}

	var saveData model.MmpSaveData
	if err := json.Unmarshal([]byte(dataStr), &saveData); err != nil {
		log.Printf("[%s] JSON decode error: %v", p.fname, err)
		return nil, nil
	}

	return &model.MmpSaveRecord{
		Data:   &saveData,
		UserID: userID,
		Sig:    sig,
		RawURL: line,
	}, nil
}

func b64URLSafeDecode(s string) ([]byte, error) {
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}
