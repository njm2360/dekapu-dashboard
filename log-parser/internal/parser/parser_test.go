package parser

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
)

var p = NewMmpLogParser("test.txt")

// --- イベント検出 ---

// 各イベントパターンに対応する行から正しい Event 定数が返される。
func TestParseLineEvents(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantEvent Event
	}{
		{"CloudLoad", "[LoadFromParsedData] something", EventCloudLoad},
		{"SessionReset", "[ResetCurrentSession] foo", EventSessionReset},
		{"WorldJoin", "[Behaviour] Joining wrld_1af53798-92a3-4c3f-99ae-a7c42ec6084d", EventWorldJoin},
		{"WorldLeave", "[OnPlayerLeft] ローカルプレイヤーが Leave した", EventWorldLeave},
		{"AppQuit", "VRCApplication: HandleApplicationQuit", EventVRChatAppQuit},
		{"JpStockover", "[JP] ストック溢れです: 1,000", EventJpStockover},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.ParseLine(tt.line)
			if got == nil {
				t.Fatal("expected non-nil result")
			}
			if got.Event != tt.wantEvent {
				t.Errorf("Event: got %d, want %d", got.Event, tt.wantEvent)
			}
		})
	}
}

// 関係のない行は nil を返す。
func TestParseLine_Unrelated_ReturnsNil(t *testing.T) {
	if got := p.ParseLine("some unrelated log line"); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

// 空行は nil を返す。
func TestParseLine_Empty_ReturnsNil(t *testing.T) {
	if got := p.ParseLine(""); got != nil {
		t.Fatalf("expected nil for empty line, got %+v", got)
	}
}

// --- JP ストック溢れ ---

// ストック溢れ行から StockoverValue が正しくパースされる。
func TestParseStockover_WithValue(t *testing.T) {
	line := "[JP] ストック溢れです: 1,234"
	got := p.ParseLine(line)
	if got == nil || got.Event != EventJpStockover {
		t.Fatal("expected JpStockover event")
	}
	if got.StockoverValue != 1234 {
		t.Errorf("StockoverValue: got %d, want 1234", got.StockoverValue)
	}
}

// 値が含まれないストック溢れ行は nil を返す。
func TestParseStockover_NoValue_ReturnsNil(t *testing.T) {
	line := "[JP] ストック溢れです"
	got := p.ParseLine(line)
	if got != nil {
		t.Fatalf("expected nil when stockover value is missing, got %+v", got)
	}
}

// --- セーブデータ URL ---

func buildV1URL(dataJSON, userID, sig string) string {
	u := &url.URL{
		Scheme: "https",
		Host:   "example.com",
		Path:   "/api/v1/data",
	}
	q := url.Values{}
	q.Set("data", dataJSON)
	q.Set("user_id", userID)
	q.Set("sig", sig)
	u.RawQuery = q.Encode()
	return u.String()
}

func buildV4URL(dataJSON, userID, sig string) string {
	encode := func(s string) string {
		encoded := base64.URLEncoding.EncodeToString([]byte(s))
		// パディングなしに正規化
		return encoded
	}
	u := &url.URL{
		Scheme: "https",
		Host:   "example.com",
		Path:   "/api/v4/data",
	}
	q := url.Values{}
	q.Set("data", encode(dataJSON))
	q.Set("user_id", encode(userID))
	q.Set("sig", sig)
	u.RawQuery = q.Encode()
	return u.String()
}

const minimalDataJSON = `{"version":1,"firstboot":0,"lastsave":0,"hide_record":0,"credit":0,"playtime":0,"credit_all":100}`

// v1 URL から UserID・Sig・RawURL・CreditAll がすべて正しくパースされる。
func TestParseSavedata_V1_Success(t *testing.T) {
	rawURL := buildV1URL(minimalDataJSON, "usr_abc123", "sig_xyz")
	got := p.ParseLine(rawURL)
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Event != EventSavedataUpdate {
		t.Fatalf("Event: got %d, want EventSavedataUpdate", got.Event)
	}
	if got.Record == nil {
		t.Fatal("expected non-nil Record")
	}
	if got.Record.UserID != "usr_abc123" {
		t.Errorf("UserID: got %s, want usr_abc123", got.Record.UserID)
	}
	if got.Record.Sig != "sig_xyz" {
		t.Errorf("Sig: got %s, want sig_xyz", got.Record.Sig)
	}
	if got.Record.RawURL != rawURL {
		t.Errorf("RawURL mismatch")
	}
	if int64(got.Record.Data.CreditAll) != 100 {
		t.Errorf("CreditAll: got %d, want 100", got.Record.Data.CreditAll)
	}
}

// v4 URL の Base64 エンコードされたパラメータが正しくデコードされる。
func TestParseSavedata_V4_Base64Decoded(t *testing.T) {
	rawURL := buildV4URL(minimalDataJSON, "usr_abc123", "sig_xyz")
	got := p.ParseLine(rawURL)
	if got == nil || got.Record == nil {
		t.Fatal("expected non-nil result and record")
	}
	if got.Record.UserID != "usr_abc123" {
		t.Errorf("UserID: got %s, want usr_abc123", got.Record.UserID)
	}
}

// data パラメータが欠如しているときは nil を返す。
func TestParseSavedata_MissingData_ReturnsNil(t *testing.T) {
	u := &url.URL{Scheme: "https", Host: "example.com", Path: "/api/v1/data"}
	q := url.Values{}
	q.Set("user_id", "usr_abc")
	q.Set("sig", "sig_xyz")
	u.RawQuery = q.Encode()

	if got := p.ParseLine(u.String()); got != nil {
		t.Fatalf("expected nil when data param is missing, got %+v", got)
	}
}

// user_id パラメータが欠如しているときは nil を返す。
func TestParseSavedata_MissingUserID_ReturnsNil(t *testing.T) {
	u := &url.URL{Scheme: "https", Host: "example.com", Path: "/api/v1/data"}
	q := url.Values{}
	q.Set("data", minimalDataJSON)
	q.Set("sig", "sig_xyz")
	u.RawQuery = q.Encode()

	if got := p.ParseLine(u.String()); got != nil {
		t.Fatalf("expected nil when user_id param is missing, got %+v", got)
	}
}

// sig パラメータが欠如しているときは nil を返す。
func TestParseSavedata_MissingSig_ReturnsNil(t *testing.T) {
	u := &url.URL{Scheme: "https", Host: "example.com", Path: "/api/v1/data"}
	q := url.Values{}
	q.Set("data", minimalDataJSON)
	q.Set("user_id", "usr_abc")
	u.RawQuery = q.Encode()

	if got := p.ParseLine(u.String()); got != nil {
		t.Fatalf("expected nil when sig param is missing, got %+v", got)
	}
}

// data が不正な JSON のときは nil を返す。
func TestParseSavedata_InvalidJSON_ReturnsNil(t *testing.T) {
	rawURL := buildV1URL("not-json", "usr_abc", "sig_xyz")
	if got := p.ParseLine(rawURL); got != nil {
		t.Fatalf("expected nil on invalid JSON, got %+v", got)
	}
}

// v4 URL の data が不正な Base64 のときは nil を返す。
func TestParseSavedata_InvalidBase64_V4_ReturnsNil(t *testing.T) {
	u := &url.URL{Scheme: "https", Host: "example.com", Path: "/api/v4/data"}
	q := url.Values{}
	q.Set("data", "!!!invalid-base64!!!")
	q.Set("user_id", base64.URLEncoding.EncodeToString([]byte("usr_abc")))
	q.Set("sig", "sig_xyz")
	u.RawQuery = q.Encode()

	if got := p.ParseLine(u.String()); got != nil {
		t.Fatalf("expected nil on invalid base64, got %+v", got)
	}
}

// --- バージョンごとのデータ検証 ---

// v1〜v4 すべてのバージョン URL が EventSavedataUpdate として正しくパースされる。
func TestParseSavedata_MultipleVersions(t *testing.T) {
	versions := []int{1, 2, 3, 4}
	for _, v := range versions {
		t.Run(fmt.Sprintf("v%d", v), func(t *testing.T) {
			var rawURL string
			if v == 4 {
				rawURL = buildV4URL(minimalDataJSON, "usr_abc", "sig")
			} else {
				u := &url.URL{
					Scheme: "https", Host: "example.com",
					Path: fmt.Sprintf("/api/v%d/data", v),
				}
				q := url.Values{}
				q.Set("data", minimalDataJSON)
				q.Set("user_id", "usr_abc")
				q.Set("sig", "sig")
				u.RawQuery = q.Encode()
				rawURL = u.String()
			}
			got := p.ParseLine(rawURL)
			if got == nil || got.Event != EventSavedataUpdate {
				t.Fatalf("v%d: expected EventSavedataUpdate, got %v", v, got)
			}
		})
	}
}

// --- FlexInt: 文字列・数値両形式 ---

// credit_all が JSON 文字列形式で渡された場合も正しく整数としてパースされる（FlexInt）。
func TestParseSavedata_FlexInt_StringValue(t *testing.T) {
	data, _ := json.Marshal(map[string]any{
		"version": 1, "firstboot": 0, "lastsave": 0,
		"hide_record": 0, "credit": 0, "playtime": 0,
		"credit_all": "999",
	})
	rawURL := buildV1URL(string(data), "usr_abc", "sig")
	got := p.ParseLine(rawURL)
	if got == nil || got.Record == nil {
		t.Fatal("expected record")
	}
	if int64(got.Record.Data.CreditAll) != 999 {
		t.Errorf("CreditAll: got %d, want 999", got.Record.Data.CreditAll)
	}
}
