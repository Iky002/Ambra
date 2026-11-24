package middleware

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func gunzipBytes(t *testing.T, raw []byte) []byte {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gzip.NewReader failed: %v", err)
	}
	defer gr.Close()
	out, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("read gunzip failed: %v", err)
	}
	return out
}

func TestGzipCompressesJSONAndTextTypes(t *testing.T) {
	payload := strings.Repeat(`{"id":"item-1","name":"Ambra Cinema","overview":"4K HDR stream"}`, 20)

	cases := []struct {
		name        string
		contentType string
		body        string
	}{
		{"json", "application/json; charset=utf-8", payload},
		{"problem_json", "application/problem+json", payload},
		{"text_plain", "text/plain; charset=utf-8", strings.Repeat("hello world\n", 40)},
		{"text_html", "text/html", strings.Repeat("<div>Ambra</div>", 40)},
		{"javascript", "application/javascript", strings.Repeat("console.log('ambra');", 40)},
		{"xml", "application/xml", strings.Repeat("<item><name>test</name></item>", 40)},
		{"svg", "image/svg+xml", strings.Repeat("<svg><circle r='10'/></svg>", 30)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.Header().Set("Content-Length", "99999")
				w.Header().Add("Vary", "Origin")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tc.body))
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
			req.Header.Set("Accept-Encoding", "br, gzip, deflate")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rec.Code)
			}
			if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
				t.Fatalf("expected Content-Encoding=gzip, got %q", got)
			}
			if got := rec.Header().Get("Content-Length"); got != "" {
				t.Fatalf("expected Content-Length to be removed, got %q", got)
			}
			varyVals := strings.Join(rec.Header().Values("Vary"), ", ")
			if !strings.Contains(varyVals, "Origin") || !strings.Contains(varyVals, "Accept-Encoding") {
				t.Fatalf("expected Vary to preserve Origin and include Accept-Encoding, got %q", varyVals)
			}

			decoded := string(gunzipBytes(t, rec.Body.Bytes()))
			if decoded != tc.body {
				t.Fatalf("gunzipped body mismatch: len=%d want=%d", len(decoded), len(tc.body))
			}
		})
	}
}

func TestGzipSkipsNonCompressibleMediaAndConditions(t *testing.T) {
	binaryBody := []byte{0x1a, 0x45, 0xdf, 0xa3, 0x9f, 0x42, 0x86, 0x81, 0x01, 0x00, 0x02, 0x03}

	skipTypes := []string{
		"video/mp4",
		"video/x-matroska",
		"audio/mpeg",
		"audio/aac",
		"image/jpeg",
		"image/png",
		"image/webp",
		"application/octet-stream",
	}

	for _, ct := range skipTypes {
		t.Run("skip_type_"+ct, func(t *testing.T) {
			h := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", ct)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(binaryBody)
			}))
			req := httptest.NewRequest(http.MethodGet, "/media", nil)
			req.Header.Set("Accept-Encoding", "gzip")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if got := rec.Header().Get("Content-Encoding"); got != "" {
				t.Fatalf("expected no compression for %s, got Content-Encoding=%q", ct, got)
			}
			if !bytes.Equal(rec.Body.Bytes(), binaryBody) {
				t.Fatalf("body modified for %s", ct)
			}
		})
	}

	t.Run("no_accept_encoding", func(t *testing.T) {
		h := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		req := httptest.NewRequest(http.MethodGet, "/json", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != `{"ok":true}` {
			t.Fatalf("should not compress without Accept-Encoding")
		}
	})

	t.Run("accept_encoding_q_zero", func(t *testing.T) {
		h := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		req := httptest.NewRequest(http.MethodGet, "/json", nil)
		req.Header.Set("Accept-Encoding", "gzip;q=0, deflate")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Header().Get("Content-Encoding") != "" {
			t.Fatalf("should not compress when gzip;q=0")
		}
	})

	t.Run("range_request_skipped", func(t *testing.T) {
		h := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("chunk"))
		}))
		req := httptest.NewRequest(http.MethodGet, "/file", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		req.Header.Set("Range", "bytes=0-4")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusPartialContent || rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != "chunk" {
			t.Fatalf("range request should not be compressed: code=%d enc=%q body=%q", rec.Code, rec.Header().Get("Content-Encoding"), rec.Body.String())
		}
	})

	t.Run("status_206_without_range_header_skipped", func(t *testing.T) {
		h := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("partial"))
		}))
		req := httptest.NewRequest(http.MethodGet, "/partial", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != "partial" {
			t.Fatalf("206 response should not be compressed")
		}
	})

	t.Run("status_204_and_304_skipped", func(t *testing.T) {
		for _, code := range []int{http.StatusNoContent, http.StatusNotModified} {
			h := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(code)
			}))
			req := httptest.NewRequest(http.MethodGet, "/empty", nil)
			req.Header.Set("Accept-Encoding", "gzip")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != code || rec.Header().Get("Content-Encoding") != "" {
				t.Fatalf("status %d should not be compressed", code)
			}
		}
	})

	t.Run("websocket_upgrade_skipped", func(t *testing.T) {
		h := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("ws-upgrade"))
		}))
		req := httptest.NewRequest(http.MethodGet, "/embywebsocket", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		req.Header.Set("Upgrade", "websocket")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != "ws-upgrade" {
			t.Fatalf("websocket upgrade should not be compressed")
		}
	})

	t.Run("already_encoded_skipped", func(t *testing.T) {
		h := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Encoding", "br")
			_, _ = w.Write([]byte("precompressed"))
		}))
		req := httptest.NewRequest(http.MethodGet, "/pre", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Header().Get("Content-Encoding") != "br" || rec.Body.String() != "precompressed" {
			t.Fatalf("pre-encoded response should not be double-compressed")
		}
	})
}

func TestGzipSniffFlushHijackAndConcurrency(t *testing.T) {
	t.Run("sniff_text_without_content_type", func(t *testing.T) {
		body := "<html><body>Ambra Streaming Server</body></html>"
		h := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Intentionally omit Content-Type and WriteHeader; let Write sniff it.
			_, _ = w.Write([]byte(body))
			// Also verify Flush works cleanly while gzip is active.
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
		}))
		req := httptest.NewRequest(http.MethodGet, "/sniff", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("expected sniffed html to be gzipped")
		}
		if !rec.Flushed {
			t.Fatalf("expected underlying ResponseWriter to be flushed")
		}
		if got := string(gunzipBytes(t, rec.Body.Bytes())); got != body {
			t.Fatalf("decoded mismatch: got %q want %q", got, body)
		}
	})

	t.Run("duplicate_writeheader_and_vary_dedup", func(t *testing.T) {
		h := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Vary", "Accept-Encoding")
			w.WriteHeader(http.StatusCreated)
			w.WriteHeader(http.StatusOK) // should be ignored
			_, _ = w.Write([]byte(`{"created":true}`))
		}))
		req := httptest.NewRequest(http.MethodPost, "/create", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", rec.Code)
		}
		if len(rec.Header().Values("Vary")) != 1 {
			t.Fatalf("expected deduplicated Vary header, got %v", rec.Header().Values("Vary"))
		}
		if got := string(gunzipBytes(t, rec.Body.Bytes())); got != `{"created":true}` {
			t.Fatalf("unexpected decoded body: %q", got)
		}
	})

	t.Run("hijack_and_unwrap", func(t *testing.T) {
		rec := httptest.NewRecorder()
		gw := &gzipResponseWriter{ResponseWriter: rec}
		if gw.Unwrap() != rec {
			t.Fatalf("Unwrap did not return underlying ResponseWriter")
		}
		if _, _, err := gw.Hijack(); err == nil {
			t.Fatalf("expected error when underlying writer is not http.Hijacker")
		}

		hjWriter := &mockHijackWriter{ResponseRecorder: httptest.NewRecorder()}
		gw2 := &gzipResponseWriter{ResponseWriter: hjWriter}
		if _, _, err := gw2.Hijack(); err != nil {
			t.Fatalf("expected hijack to succeed, got %v", err)
		}
		if !hjWriter.hijacked {
			t.Fatalf("expected underlying Hijack to be called")
		}
	})

	t.Run("concurrent_pool_safety", func(t *testing.T) {
		h := Gzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","items":[1,2,3,4,5]}`))
		}))

		var wg sync.WaitGroup
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodGet, "/concurrent", nil)
				req.Header.Set("Accept-Encoding", "gzip")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if rec.Header().Get("Content-Encoding") != "gzip" {
					t.Errorf("missing gzip header")
					return
				}
				if got := string(gunzipBytes(t, rec.Body.Bytes())); got != `{"status":"ok","items":[1,2,3,4,5]}` {
					t.Errorf("unexpected decoded body: %q", got)
				}
			}()
		}
		wg.Wait()
	})
}

type mockHijackWriter struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (m *mockHijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	m.hijacked = true
	return nil, nil, nil
}
