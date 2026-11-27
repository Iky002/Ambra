package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// DefaultMaxJSONBodyBytes caps untrusted JSON request bodies at 4 MiB.
const DefaultMaxJSONBodyBytes int64 = 4 << 20

// ErrBodyTooLarge is returned when a JSON request body exceeds maxBytes.
var ErrBodyTooLarge = errors.New("request body exceeds maximum allowed size")

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// DecodeJSONLimit decodes a JSON request body bounded by maxBytes (defaults to 4 MiB).
// If the body exceeds maxBytes bytes, ErrBodyTooLarge is returned explicitly.
func DecodeJSONLimit(r *http.Request, dst any, maxBytes int64) error {
	if r == nil || r.Body == nil {
		return io.EOF
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxJSONBodyBytes
	}
	cr := &countingReader{r: io.LimitReader(r.Body, maxBytes+1)}
	err := json.NewDecoder(cr).Decode(dst)
	if cr.n > maxBytes {
		return ErrBodyTooLarge
	}
	if err != nil {
		return err
	}
	if _, copyErr := io.Copy(io.Discard, cr); copyErr != nil {
		return copyErr
	}
	if cr.n > maxBytes {
		return ErrBodyTooLarge
	}
	return nil
}

// DecodeJSONMaxBytes wraps r.Body in http.MaxBytesReader and decodes the JSON payload into dst.
// Oversized bodies return an error matching both errors.Is(err, ErrBodyTooLarge) and *http.MaxBytesError.
func DecodeJSONMaxBytes(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	if r == nil || r.Body == nil {
		return io.EOF
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxJSONBodyBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	err := json.NewDecoder(r.Body).Decode(dst)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return fmt.Errorf("%w: %w", ErrBodyTooLarge, err)
		}
		return err
	}
	if _, copyErr := io.Copy(io.Discard, r.Body); copyErr != nil {
		var maxErr *http.MaxBytesError
		if errors.As(copyErr, &maxErr) {
			return fmt.Errorf("%w: %w", ErrBodyTooLarge, copyErr)
		}
		return copyErr
	}
	return nil
}

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func Error(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"Error":   http.StatusText(status),
		"Message": msg,
	})
}

// Query wraps url.Values with Emby-compatible case-insensitive lookup.
type Query struct {
	values map[string][]string
}

func ParseQuery(r *http.Request) Query {
	raw := r.URL.Query()
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	m := make(map[string][]string, len(raw))
	for _, k := range keys {
		lk := strings.ToLower(k)
		m[lk] = append(m[lk], raw[k]...)
	}
	return Query{values: m}
}

func (q Query) Get(key string) string {
	vs := q.values[strings.ToLower(key)]
	if len(vs) == 0 {
		return ""
	}
	return vs[0]
}

func (q Query) Has(key string) bool {
	_, ok := q.values[strings.ToLower(key)]
	return ok
}

func (q Query) Int(key string, def int) int {
	s := q.Get(key)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func (q Query) Int64(key string, def int64) int64 {
	s := q.Get(key)
	if s == "" {
		return def
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func (q Query) Bool(key string) *bool {
	s := strings.ToLower(strings.TrimSpace(q.Get(key)))
	if s == "" {
		return nil
	}
	b := s == "true" || s == "1" || s == "yes"
	return &b
}

// List splits both repeated query keys and comma-separated values.
func (q Query) List(key string) []string {
	vs := q.values[strings.ToLower(key)]
	if len(vs) == 0 {
		return nil
	}
	var out []string
	for _, v := range vs {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func (q Query) Raw() url.Values {
	out := make(url.Values, len(q.values))
	for k, v := range q.values {
		out[k] = v
	}
	return out
}
