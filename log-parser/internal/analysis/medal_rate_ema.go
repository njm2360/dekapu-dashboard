package analysis

import (
	"math"
	"time"
)

type MedalRateEMA struct {
	decayConst    float64
	lastTimestamp *time.Time
	lastTotal     *int64
	emaRate       float64
	offsetTotal   int64
}

func NewMedalRateEMA(decayConst float64) *MedalRateEMA {
	return &MedalRateEMA{decayConst: decayConst}
}

func (e *MedalRateEMA) AddOffset(value int64) {
	e.offsetTotal += value
}

func (e *MedalRateEMA) Update(total int64, ts time.Time) *int64 {
	adjusted := total - e.offsetTotal

	if e.lastTimestamp == nil {
		e.lastTimestamp = &ts
		e.lastTotal = &adjusted
		return nil
	}

	dt := ts.Sub(*e.lastTimestamp).Seconds()
	if dt <= 0 {
		r := int64(e.emaRate)
		return &r
	}

	delta := float64(adjusted - *e.lastTotal)
	instantRate := (delta / dt) * 60.0 // medals/min
	alpha := 1 - math.Exp(-dt/e.decayConst)
	e.emaRate = (1-alpha)*e.emaRate + alpha*instantRate

	e.lastTimestamp = &ts
	e.lastTotal = &adjusted

	r := int64(e.emaRate)
	return &r
}

func (e *MedalRateEMA) Reset() {
	e.lastTimestamp = nil
	e.lastTotal = nil
	e.emaRate = 0
	e.offsetTotal = 0
}
