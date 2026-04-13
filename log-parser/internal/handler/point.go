package handler

import (
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api/write"

	"log-parser/internal/model"
)

func newSavedataPoint(record *model.MmpSaveRecord, creditDelta *int64) *write.Point {
	p := influxdb2.NewPointWithMeasurement("mpp-savedata").
		AddTag("user", record.UserID).
		SetTime(record.Data.Lastsave.Time()).
		AddField("l_achieve_count", int64(len(record.Data.LAchieve)))

	if creditDelta != nil {
		p.AddField("credit_all_delta_1m", *creditDelta)
	}

	for k, v := range record.Data.DumpForInflux() {
		p.AddField(k, v)
	}

	return p
}
