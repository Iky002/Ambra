package middleware

import (
	"bufio"
	"compress/gzip"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

var gzipWriterPool = sync.Pool{
	New: func() any {
		return gzip.NewWriter(io.Discard)
	},
}

// Gzip transparently compresses compressible HTTP responses (JSON, text, JS, XML, SVG)
// when the client advertises gzip support via Accept-Encoding.
// It automatically skips Range requests, WebSocket upgrades, 204/304/206 responses,
// already-encoded responses, and binary/media content types (video/*, audio/*, raster images, octet-stream).
func Gzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsGzip(r.Header.Get("Accept-Encoding")) ||
			r.Header.Get("Upgrade") != "" ||
			r.Header.Get("Range") != "" {
			next.ServeHTTP(w, r)
			return
		}

		gw := &gzipResponseWriter{
			ResponseWriter: w,
		}
		defer gw.close()

		next.ServeHTTP(gw, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	decided     bool
	compress    bool
	wroteHeader bool
}

func (w *gzipResponseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.decide(statusCode, nil)
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.decide(http.StatusOK, b)
		w.wroteHeader = true
		w.ResponseWriter.WriteHeader(http.StatusOK)
	}
	if w.compress && w.gz != nil {
		return w.gz.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

func (w *gzipResponseWriter) Flush() {
	if w.compress && w.gz != nil {
		_ = w.gz.Flush()
	}
	if fl, ok := w.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

func (w *gzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("middleware: underlying ResponseWriter does not implement http.Hijacker")
}

// Unwrap exposes the underlying ResponseWriter for http.ResponseController.
func (w *gzipResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *gzipResponseWriter) decide(statusCode int, sniff []byte) {
	if w.decided {
		return
	}
	w.decided = true

	// Never compress informational, empty, not-modified, or partial-content responses.
	if statusCode < http.StatusOK ||
		statusCode == http.StatusNoContent ||
		statusCode == http.StatusNotModified ||
		statusCode == http.StatusPartialContent {
		return
	}

	h := w.Header()
	if h.Get("Content-Encoding") != "" || h.Get("Content-Range") != "" {
		return
	}

	ct := h.Get("Content-Type")
	if ct == "" && len(sniff) > 0 {
		ct = http.DetectContentType(sniff)
		h.Set("Content-Type", ct)
	}

	if !isCompressibleContentType(ct) {
		return
	}

	gz := gzipWriterPool.Get().(*gzip.Writer)
	gz.Reset(w.ResponseWriter)
	w.gz = gz
	w.compress = true

	h.Set("Content-Encoding", "gzip")
	addVaryHeader(h, "Accept-Encoding")
	h.Del("Content-Length")
}

func (w *gzipResponseWriter) close() {
	if w.gz == nil {
		return
	}
	_ = w.gz.Close()
	w.gz.Reset(io.Discard)
	gzipWriterPool.Put(w.gz)
	w.gz = nil
}

// acceptsGzip checks whether the Accept-Encoding header permits gzip (excluding q=0).
func acceptsGzip(header string) bool {
	if header == "" {
		return false
	}
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		segments := strings.Split(part, ";")
		encoding := strings.ToLower(strings.TrimSpace(segments[0]))
		if encoding != "gzip" {
			continue
		}
		qZero := false
		for _, param := range segments[1:] {
			param = strings.ToLower(strings.TrimSpace(param))
			if strings.HasPrefix(param, "q=") {
				val := strings.TrimSpace(strings.TrimPrefix(param, "q="))
				if val == "0" || val == "0.0" || val == "0.00" || val == "0.000" {
					qZero = true
				}
			}
		}
		if !qZero {
			return true
		}
	}
	return false
}

// isCompressibleContentType returns true only for text/structured web payloads.
func isCompressibleContentType(contentType string) bool {
	if contentType == "" {
		return false
	}
	mediaType := contentType
	if idx := strings.IndexByte(mediaType, ';'); idx >= 0 {
		mediaType = mediaType[:idx]
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if mediaType == "" {
		return false
	}

	if strings.HasPrefix(mediaType, "text/") {
		return true
	}

	switch mediaType {
	case "application/json",
		"application/javascript",
		"application/x-javascript",
		"application/xml",
		"image/svg+xml":
		return true
	}

	if strings.HasPrefix(mediaType, "application/") &&
		(strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml")) {
		return true
	}

	return false
}

func addVaryHeader(h http.Header, token string) {
	for _, existing := range h.Values("Vary") {
		for _, part := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return
			}
		}
	}
	h.Add("Vary", token)
}
