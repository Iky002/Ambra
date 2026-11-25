package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestResponses(t *testing.T) {
	// JSON
	rec := httptest.NewRecorder()
	JSON(rec, http.StatusCreated, map[string]string{"ok": "yes"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("unexpected Content-Type: %q", ct)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["ok"] != "yes" {
		t.Fatalf("unexpected JSON body: %q (%v)", rec.Body.String(), err)
	}

	// NoContent
	rec2 := httptest.NewRecorder()
	NoContent(rec2)
	if rec2.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec2.Code)
	}

	// Error
	rec3 := httptest.NewRecorder()
	Error(rec3, http.StatusNotFound, "missing item")
	if rec3.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec3.Code)
	}
	var errBody map[string]string
	if err := json.Unmarshal(rec3.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if errBody["Error"] != "Not Found" || errBody["Message"] != "missing item" {
		t.Fatalf("unexpected error body: %+v", errBody)
	}
}

func TestParseQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/Items?Limit=25&startIndex=bad&Big=9223372036854775800&BadBig=xyz&Recursive=true&Flag1=1&FlagYes=YES&FlagFalse=false&Empty=&Types=Movie,Series&types=Episode,+,Audio", nil)
	q := ParseQuery(req)

	if !q.Has("limit") || !q.Has("LIMIT") {
		t.Fatal("expected Has to be case-insensitive")
	}
	if q.Has("nonexistent") {
		t.Fatal("expected nonexistent key to be false")
	}
	if got := q.Get("LIMIT"); got != "25" {
		t.Fatalf("Get(LIMIT) = %q, want 25", got)
	}
	if got := q.Get("missing"); got != "" {
		t.Fatalf("Get(missing) = %q, want empty", got)
	}

	// Int
	if got := q.Int("limit", 10); got != 25 {
		t.Fatalf("Int(limit) = %d, want 25", got)
	}
	if got := q.Int("startIndex", 10); got != 10 {
		t.Fatalf("Int(bad) = %d, want default 10", got)
	}
	if got := q.Int("missing", 7); got != 7 {
		t.Fatalf("Int(missing) = %d, want default 7", got)
	}

	// Int64
	if got := q.Int64("big", 0); got != 9223372036854775800 {
		t.Fatalf("Int64(big) = %d", got)
	}
	if got := q.Int64("badbig", 42); got != 42 {
		t.Fatalf("Int64(badbig) = %d, want 42", got)
	}
	if got := q.Int64("missing", 99); got != 99 {
		t.Fatalf("Int64(missing) = %d, want 99", got)
	}

	// Bool
	if b := q.Bool("recursive"); b == nil || !*b {
		t.Fatalf("Bool(recursive) = %v, want true", b)
	}
	if b := q.Bool("flag1"); b == nil || !*b {
		t.Fatalf("Bool(flag1) = %v, want true", b)
	}
	if b := q.Bool("flagyes"); b == nil || !*b {
		t.Fatalf("Bool(flagyes) = %v, want true", b)
	}
	if b := q.Bool("flagfalse"); b == nil || *b {
		t.Fatalf("Bool(flagfalse) = %v, want false", b)
	}
	if b := q.Bool("empty"); b != nil {
		t.Fatalf("Bool(empty) = %v, want nil", *b)
	}
	if b := q.Bool("missing"); b != nil {
		t.Fatalf("Bool(missing) = %v, want nil", *b)
	}

	// List (sort both slices since map iteration across mixed-case keys is unordered)
	gotList := append([]string(nil), q.List("TYPES")...)
	wantList := []string{"Movie", "Series", "Episode", "Audio"}
	sort.Strings(gotList)
	sort.Strings(wantList)
	if !reflect.DeepEqual(gotList, wantList) {
		t.Fatalf("List(TYPES) = %#v, want %#v", gotList, wantList)
	}
	if got := q.List("missing"); got != nil {
		t.Fatalf("List(missing) = %#v, want nil", got)
	}

	// Raw
	raw := q.Raw()
	if raw.Get("limit") != "25" {
		t.Fatalf("Raw().Get(limit) = %q, want 25", raw.Get("limit"))
	}
}

func TestDecodeJSONLimitNilAndLimits(t *testing.T) {
	var dst map[string]string

	// nil request -> io.EOF
	if err := DecodeJSONLimit(nil, &dst, 0); !errors.Is(err, io.EOF) {
		t.Fatalf("DecodeJSONLimit(nil) = %v, want io.EOF", err)
	}

	// nil request body -> io.EOF
	reqNilBody := &http.Request{Method: http.MethodPost, Body: nil}
	if err := DecodeJSONLimit(reqNilBody, &dst, 128); !errors.Is(err, io.EOF) {
		t.Fatalf("DecodeJSONLimit(nil body) = %v, want io.EOF", err)
	}

	// Valid JSON with default limit (maxBytes <= 0)
	reqOK := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"hello":"world"}`))
	if err := DecodeJSONLimit(reqOK, &dst, 0); err != nil {
		t.Fatalf("DecodeJSONLimit valid failed: %v", err)
	}
	if dst["hello"] != "world" {
		t.Fatalf("unexpected decoded payload: %+v", dst)
	}

	// Payload exceeding small limit -> decode error
	reqTooLarge := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"hello":"`+strings.Repeat("a", 64)+`"}`))
	var dst2 map[string]string
	if err := DecodeJSONLimit(reqTooLarge, &dst2, 16); err == nil {
		t.Fatal("expected error when JSON body exceeds maxBytes")
	}
}
