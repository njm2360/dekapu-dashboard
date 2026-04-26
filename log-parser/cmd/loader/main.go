package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api/write"
	"github.com/joho/godotenv"
	"github.com/schollz/progressbar/v3"

	"log-parser/internal/analysis"
	"log-parser/internal/envutil"
	"log-parser/internal/influx"
	"log-parser/internal/model"
	"log-parser/internal/parser"
)

func main() {
	_ = godotenv.Load()

	logDir := envutil.Require("VRCHAT_LOG_DIR")

	opts := influxdb2.DefaultOptions().
		SetHTTPRequestTimeout(uint(10 * time.Second / time.Millisecond))
	client := influxdb2.NewClientWithOptions(
		envutil.Require("INFLUXDB_URL"),
		envutil.Require("INFLUXDB_TOKEN"),
		opts,
	)
	defer client.Close()

	writeAPI := client.WriteAPIBlocking(envutil.Require("INFLUXDB_ORG"), envutil.Require("INFLUXDB_BUCKET"))

	scanner := bufio.NewScanner(os.Stdin)

	startDT := promptDatetime(scanner, "開始日時 (YYYYMMDDHHmmss, 空白=制限なし): ")
	endDT := promptDatetime(scanner, "終了日時 (YYYYMMDDHHmmss, 空白=制限なし): ")

	if !confirmSettings(scanner, startDT, endDT) {
		return
	}

	files := collectLogFiles(logDir)
	if len(files) == 0 {
		log.Printf("[Loader] No log files found in %s", logDir)
		return
	}

	total := 0
	for i, fpath := range files {
		fname := filepath.Base(fpath)
		p := parser.NewMmpLogParser(fname)
		medalRate := analysis.NewMedalRateEMA(20.0)

		info, err := os.Stat(fpath)
		if err != nil {
			log.Fatalf("[Loader] Cannot stat %s: %v", fname, err)
		}

		bar := progressbar.NewOptions64(
			info.Size(),
			progressbar.OptionSetDescription(fmt.Sprintf("[%d/%d] %s", i+1, len(files), fname)),
			progressbar.OptionSetWidth(40),
			progressbar.OptionShowBytes(true),
			progressbar.OptionShowCount(),
			progressbar.OptionOnCompletion(func() { fmt.Println() }),
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        "=",
				SaucerHead:    ">",
				SaucerPadding: " ",
				BarStart:      "[",
				BarEnd:        "]",
			}),
		)

		if err := processFile(fpath, fname, p, medalRate, writeAPI, startDT, endDT, &total, bar); err != nil {
			log.Fatalf("[Loader] Aborting due to write error: %v", err)
		}
	}

	log.Printf("[Loader] Finished. Written points: %d", total)
}

func processFile(
	fpath, fname string,
	p *parser.MmpLogParser,
	medalRate *analysis.MedalRateEMA,
	writeAPI influx.BlockingWriteAPI,
	startDT, endDT *time.Time,
	total *int,
	bar *progressbar.ProgressBar,
) error {
	f, err := os.Open(fpath)
	if err != nil {
		log.Printf("[Loader] Cannot open %s: %v", fname, err)
		return nil
	}
	defer f.Close()

	sc := bufio.NewScanner(io.TeeReader(f, bar))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		result := p.ParseLine(line)
		if result == nil {
			continue
		}

		switch result.Event {
		case parser.EventJpStockover:
			medalRate.AddOffset(result.StockoverValue)
		case parser.EventCloudLoad, parser.EventSessionReset, parser.EventWorldJoin:
			medalRate.Reset()
		case parser.EventSavedataUpdate:
			if result.Record == nil {
				continue
			}
			rec := result.Record
			ts := rec.Data.Lastsave.Time()

			if startDT != nil && ts.Before(*startDT) {
				continue
			}
			if endDT != nil && ts.After(*endDT) {
				continue
			}

			delta := medalRate.Update(int64(rec.Data.CreditAll), ts)

			if err := writeWithRetry(writeAPI, rec, delta); err != nil {
				return fmt.Errorf("InfluxDB write failed for %s: %w", fname, err)
			}
			*total++
		}
	}
	if err := sc.Err(); err != nil {
		log.Printf("[Loader] Scan error %s: %v", fname, err)
	}
	return nil
}

func writeWithRetry(api influx.BlockingWriteAPI, rec *model.MmpSaveRecord, delta *int64) error {
	p := newSavedataPoint(rec, delta)
	const maxRetry = 3
	for i := range maxRetry {
		if err := api.WritePoint(context.Background(), p); err == nil {
			return nil
		} else {
			log.Printf("[Loader] Write attempt %d failed: %v", i+1, err)
		}
		if i < maxRetry-1 {
			time.Sleep(3 * time.Second)
		}
	}
	return fmt.Errorf("write failed after %d attempts", maxRetry)
}

// collectLogFiles returns output_log_*.txt files sorted ascending by path.
func collectLogFiles(logDir string) []string {
	entries, err := filepath.Glob(filepath.Join(logDir, "output_log_*.txt"))
	if err != nil {
		return nil
	}
	sort.Strings(entries)
	return entries
}

func promptDatetime(sc *bufio.Scanner, prompt string) *time.Time {
	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	for {
		fmt.Print(prompt)
		sc.Scan()
		s := strings.TrimSpace(sc.Text())
		if s == "" {
			return nil
		}
		t, err := time.ParseInLocation("20060102150405", s, jst)
		if err != nil {
			fmt.Println("フォーマットが不正です。YYYYMMDDHHmmss 形式で入力してください。")
			continue
		}
		utc := t.UTC()
		return &utc
	}
}

func confirmSettings(sc *bufio.Scanner, start, end *time.Time) bool {
	jst := time.FixedZone("Asia/Tokyo", 9*60*60)
	fmtTime := func(t *time.Time) string {
		if t == nil {
			return "制限なし"
		}
		return t.In(jst).Format("2006-01-02 15:04:05")
	}
	for {
		fmt.Printf("開始: %s\n", fmtTime(start))
		fmt.Printf("終了: %s\n", fmtTime(end))
		fmt.Print("この設定で開始しますか？ [Y/n]: ")
		sc.Scan()
		switch strings.ToLower(strings.TrimSpace(sc.Text())) {
		case "y":
			return true
		case "n":
			fmt.Println("処理を中止しました。")
			return false
		default:
			fmt.Println("Y または n を入力してください。")
		}
	}
}

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
