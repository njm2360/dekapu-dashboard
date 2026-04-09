package handler

import (
	"log"

	"log-parser/internal/analysis"
	"log-parser/internal/influx"
	"log-parser/internal/model"
	"log-parser/internal/parser"
	"log-parser/internal/watcher"
)

type SaveManager interface {
	TrySave(record *model.MmpSaveRecord, ignoreRateLimit bool) bool
}

type handler struct {
	label          string
	parser         *parser.MmpLogParser
	medalRate      *analysis.MedalRateEMA
	influx         influx.PointWriter
	autoSave       SaveManager
	enableAutosave bool

	lastRecord       *model.MmpSaveRecord
	pendingLeaveSave bool
	hasUnsavedRecord bool
}

func NewHandler(label string, w influx.PointWriter, a SaveManager, enableAutosave bool) watcher.LineHandler {
	h := &handler{
		label:          label,
		parser:         parser.NewMmpLogParser(label),
		medalRate:      analysis.NewMedalRateEMA(20.0),
		influx:         w,
		autoSave:       a,
		enableAutosave: enableAutosave,
	}
	return func(_ string, line string) {
		h.handleLine(line)
	}
}

func (h *handler) handleLine(line string) {
	result := h.parser.ParseLine(line)
	if result == nil {
		return
	}

	switch result.Event {
	case parser.EventJpStockover:
		h.medalRate.AddOffset(result.StockoverValue)

	case parser.EventCloudLoad:
		log.Printf("[%s] Cloud load detected. Reset medal rate.", h.label)
		h.medalRate.Reset()

	case parser.EventSessionReset:
		log.Printf("[%s] Session reset detected. Reset medal rate.", h.label)
		h.medalRate.Reset()

	case parser.EventWorldJoin:
		log.Printf("[%s] World join detected. Reset medal rate.", h.label)
		h.medalRate.Reset()

	case parser.EventWorldLeave:
		log.Printf("[%s] World leave detected.", h.label)
		if h.enableAutosave {
			log.Printf("[%s] Waiting for leave save log.", h.label)
			h.pendingLeaveSave = true
		}

	case parser.EventVRChatAppQuit:
		log.Printf("[%s] App quit detected.", h.label)
		if h.enableAutosave && h.lastRecord != nil && h.hasUnsavedRecord {
			log.Printf("[%s] Saving unsaved record on quit.", h.label)
			if h.autoSave.TrySave(h.lastRecord, true) {
				h.hasUnsavedRecord = false
			}
		}

	case parser.EventSavedataUpdate:
		h.handleSavedataUpdate(result.Record)
	}
}

func (h *handler) handleSavedataUpdate(record *model.MmpSaveRecord) {
	if record == nil {
		return
	}
	h.lastRecord = record

	delta := h.medalRate.Update(int64(record.Data.CreditAll), record.Data.Lastsave.Time())

	h.influx.WritePoint(newSavedataPoint(record, delta))

	if !h.enableAutosave {
		return
	}

	if h.pendingLeaveSave {
		h.pendingLeaveSave = false
		if h.autoSave.TrySave(record, true) {
			h.hasUnsavedRecord = false
		}
		return
	}

	h.hasUnsavedRecord = !h.autoSave.TrySave(record, false)
}
