package httpx

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// DefaultMaxJSONBodyBytes caps untrusted JSON request bodies at 4 MiB.
const DefaultMaxJSONBodyBytes int64 = 4 << 20

// DecodeJSONLimit decodes a JSON request body bounded by maxBytes (defaults to 4 MiB).
func DecodeJSONLimit(r *http.Request, dst any, maxBytes int64) error {
	if r == nil || r.Body == nil {
		return io.EOF
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxJSONBodyBytes
	}
	return json.NewDecoder(io.LimitReader(r.Body, maxBytes)).Decode(dst)
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
