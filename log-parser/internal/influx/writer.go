package influx

import (
	"context"
	"log"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
)

type PointWriter interface {
	WritePoint(point *write.Point)
}

type BlockingWriteAPI interface {
	WritePoint(ctx context.Context, point ...*write.Point) error
}

type Writer struct {
	client   influxdb2.Client
	writeAPI api.WriteAPI
}

func NewWriter(influxURL, token, org, bucket string) *Writer {
	opts := influxdb2.DefaultOptions().
		SetHTTPRequestTimeout(uint(5 * time.Second / time.Millisecond))
	client := influxdb2.NewClientWithOptions(influxURL, token, opts)
	writeAPI := client.WriteAPI(org, bucket)

	w := &Writer{client: client, writeAPI: writeAPI}

	go func() {
		for err := range writeAPI.Errors() {
			log.Printf("[InfluxDB] Write error: %v", err)
		}
	}()

	return w
}

func (w *Writer) WritePoint(point *write.Point) {
	w.writeAPI.WritePoint(point)
}

func (w *Writer) Flush() {
	w.writeAPI.Flush()
}

func (w *Writer) Close() {
	w.writeAPI.Flush()
	w.client.Close()
}
