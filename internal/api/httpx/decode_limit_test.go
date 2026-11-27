package httpx

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestL02_DecodeJSONLimitDistinguishesBodyTooLarge(t *testing.T) {
	type sample struct {
		Name string `json:"name"`
	}

	// Empty body still returns io.EOF for optional-body callers.
	reqEmpty := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(""))
	var dst sample
	if err := DecodeJSONLimit(reqEmpty, &dst, 64); !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF for empty body, got %v", err)
	}

	// Payload within maxBytes succeeds.
	reqOK := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"name":"ambra"}`))
	if err := DecodeJSONLimit(reqOK, &dst, 64); err != nil || dst.Name != "ambra" {
		t.Fatalf("expected valid decode, got err=%v dst=%+v", err, dst)
	}

	// Valid JSON payload exceeding maxBytes returns ErrBodyTooLarge (not misleading syntax error).
	bigBody := `{"name":"` + strings.Repeat("x", 128) + `"}`
	reqTooBig := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(bigBody))
	err := DecodeJSONLimit(reqTooBig, &dst, 32)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("expected ErrBodyTooLarge, got %v", err)
	}

	// DecodeJSONMaxBytes also returns ErrBodyTooLarge via http.MaxBytesReader.
	rec := httptest.NewRecorder()
	reqMax := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(bigBody))
	if err := DecodeJSONMaxBytes(rec, reqMax, &dst, 32); !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("expected ErrBodyTooLarge from DecodeJSONMaxBytes, got %v", err)
	}
}
