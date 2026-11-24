package httpx

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

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
	m := make(map[string][]string, len(raw))
	for k, v := range raw {
		lk := strings.ToLower(k)
		m[lk] = append(m[lk], v...)
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
