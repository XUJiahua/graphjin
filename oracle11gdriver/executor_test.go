package oracle11gdriver

import (
	"encoding/json"
	"testing"
	"time"

	go_ora "github.com/sijms/go-ora/v2"
)

func TestNormalizeValueUsesColumnType(t *testing.T) {
	got := normalizeValue("123", "number")
	if v, ok := got.(json.Number); !ok || v.String() != "123" {
		t.Fatalf("normalizeValue(number) = %#v, want json.Number(%q)", got, "123")
	}

	got = normalizeValue("000123", "varchar2")
	if v, ok := got.(string); !ok || v != "000123" {
		t.Fatalf("normalizeValue(varchar2) = %#v, want %q", got, "000123")
	}

	got = normalizeValue("12.5", "number")
	if v, ok := got.(json.Number); !ok || v.String() != "12.5" {
		t.Fatalf("normalizeValue(decimal) = %#v, want json.Number(%q)", got, "12.5")
	}

	got = normalizeValue("123456789012345678901234567890", "number")
	if v, ok := got.(json.Number); !ok || v.String() != "123456789012345678901234567890" {
		t.Fatalf("normalizeValue(bigint) = %#v, want exact json.Number", got)
	}

	b, err := json.Marshal(map[string]any{"n": got})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"n":123456789012345678901234567890}` {
		t.Fatalf("json.Marshal(bigint) = %s, want exact numeric literal", string(b))
	}
}

func TestNormalizeValueFormatsOracleTemporalTypes(t *testing.T) {
	ts := time.Date(2026, 3, 14, 15, 9, 26, 123000000, time.FixedZone("UTC+8", 8*60*60))

	got := normalizeValue(ts, "date")
	if v, ok := got.(string); !ok || v != "2026-03-14T15:09:26" {
		t.Fatalf("normalizeValue(date) = %#v, want %q", got, "2026-03-14T15:09:26")
	}

	got = normalizeValue(ts, "timestamp")
	if v, ok := got.(string); !ok || v != "2026-03-14T15:09:26.123" {
		t.Fatalf("normalizeValue(timestamp) = %#v, want %q", got, "2026-03-14T15:09:26.123")
	}

	got = normalizeValue(ts, "timestamp with time zone")
	if v, ok := got.(string); !ok || v != "2026-03-14T15:09:26.123+08:00" {
		t.Fatalf("normalizeValue(timestamp with time zone) = %#v, want %q", got, "2026-03-14T15:09:26.123+08:00")
	}
}

func TestNormalizeRowValuePreservesNumericBindType(t *testing.T) {
	jsonValue, bindValue := normalizeRowValue("123456789012345678901234567890", "number")

	if v, ok := jsonValue.(json.Number); !ok || v.String() != "123456789012345678901234567890" {
		t.Fatalf("jsonValue = %#v, want exact json.Number", jsonValue)
	}

	num, ok := bindValue.(*go_ora.Number)
	if !ok {
		t.Fatalf("bindValue type = %T, want *go_ora.Number", bindValue)
	}

	got, err := num.String()
	if err != nil {
		t.Fatal(err)
	}
	if got != "123456789012345678901234567890" {
		t.Fatalf("bindValue.String() = %q, want exact numeric string", got)
	}

	_, bindValue = normalizeRowValue([]byte("42"), "number")
	if _, ok := bindValue.(*go_ora.Number); !ok {
		t.Fatalf("bindValue([]byte) type = %T, want *go_ora.Number", bindValue)
	}
}

func TestResolveCursorParamAcceptsPrefixedAndStrippedCursor(t *testing.T) {
	info := &queryCursor{
		ParamName: "products_cursor",
		Prefix:    "gj-test:",
		SelID:     7,
		OrderBy: []queryCursorColumn{
			{Source: "price", ValueType: "number"},
			{Source: "created_at", ValueType: "timestamp"},
		},
	}

	param := queryParam{ArgIndex: 0, CursorIdx: 1, ValueType: "timestamp", Type: "cursor"}
	got, err := resolveCursorParam(info, param, "gj-test:7,12.5,2026-03-14T15:09:26.123456789")
	if err != nil {
		t.Fatal(err)
	}
	ts, ok := got.(time.Time)
	if !ok {
		t.Fatalf("prefixed cursor bind = %T, want time.Time", got)
	}
	if want := time.Date(2026, 3, 14, 15, 9, 26, 123456789, time.UTC); !ts.Equal(want) {
		t.Fatalf("prefixed cursor bind = %v, want %v", ts, want)
	}

	param = queryParam{ArgIndex: 0, CursorIdx: 0, ValueType: "number", Type: "cursor"}
	got, err = resolveCursorParam(info, param, "7,12345678901234567890,2026-03-14T15:09:26.123456789")
	if err != nil {
		t.Fatal(err)
	}
	num, ok := got.(*go_ora.Number)
	if !ok {
		t.Fatalf("stripped cursor bind = %T, want *go_ora.Number", got)
	}
	s, err := num.String()
	if err != nil {
		t.Fatal(err)
	}
	if s != "12345678901234567890" {
		t.Fatalf("numeric cursor bind = %q, want exact value", s)
	}
}

func TestBuildCursorValueUsesPrefixAndExactOrderValues(t *testing.T) {
	info := &queryCursor{
		Prefix: "gj-test:",
		SelID:  11,
		OrderBy: []queryCursorColumn{
			{Source: "price", ValueType: "number"},
			{Source: "created_at", ValueType: "timestamp with time zone"},
		},
	}

	num, err := go_ora.NewNumberFromString("12345678901234567890")
	if err != nil {
		t.Fatal(err)
	}

	rows := []*rowData{
		{
			Cols: map[string]any{
				"price":      num,
				"created_at": time.Date(2026, 3, 14, 15, 9, 26, 123000000, time.FixedZone("UTC+8", 8*60*60)),
			},
		},
	}

	got := buildCursorValue(info, rows)
	want := "gj-test:11,12345678901234567890,2026-03-14T15:09:26.123+08:00"
	if got != want {
		t.Fatalf("buildCursorValue() = %v, want %v", got, want)
	}
}
