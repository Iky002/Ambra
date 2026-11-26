package livetv

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// M3UChannel represents a single channel entry parsed from an M3U/M3U8 playlist.
type M3UChannel struct {
	TvgID      string `json:"TvgId,omitempty"`
	TvgName    string `json:"TvgName,omitempty"`
	TvgLogo    string `json:"TvgLogo,omitempty"`
	GroupTitle string `json:"GroupTitle,omitempty"`
	Number     string `json:"Number,omitempty"`
	Name       string `json:"Name"`
	URL        string `json:"Url"`
}

// maxM3UChannels caps the number of channels parsed from a single M3U playlist to prevent OOM.
const maxM3UChannels = 10000

// ParseM3U parses an M3U/M3U8 playlist from r and extracts channel metadata and stream URLs.
func ParseM3U(r io.Reader) ([]M3UChannel, error) {
	scanner := bufio.NewScanner(r)
	// Support long playlist lines (e.g. embedded logos or long query strings).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var channels []M3UChannel
	var pending *M3UChannel
	autoNum := 1

	for scanner.Scan() {
		if len(channels) >= maxM3UChannels {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		line = strings.TrimPrefix(line, "\uFEFF") // Strip UTF-8 BOM if present.
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#EXTINF:") {
			ch := parseExtInf(strings.TrimPrefix(line, "#EXTINF:"))
			pending = &ch
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}

		var ch M3UChannel
		if pending != nil {
			ch = *pending
			pending = nil
		}
		ch.URL = line
		if ch.Name == "" {
			if ch.TvgName != "" {
				ch.Name = ch.TvgName
			} else {
				ch.Name = line
			}
		}
		if ch.Number == "" {
			ch.Number = strconv.Itoa(autoNum)
		}
		autoNum++
		channels = append(channels, ch)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return channels, nil
}

// ParseM3UString parses an M3U playlist directly from a string.
func ParseM3UString(content string) ([]M3UChannel, error) {
	return ParseM3U(strings.NewReader(content))
}

func parseExtInf(rest string) M3UChannel {
	var ch M3UChannel

	// Split metadata section from trailing channel display name on the first comma outside quotes.
	metaPart := rest
	inQuotes := false
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '"':
			inQuotes = !inQuotes
		case ',':
			if !inQuotes {
				metaPart = rest[:i]
				ch.Name = strings.TrimSpace(rest[i+1:])
				goto parseAttrs
			}
		}
	}

parseAttrs:
	attrs := parseAttributes(metaPart)
	ch.TvgID = attrs["tvg-id"]
	ch.TvgName = attrs["tvg-name"]
	ch.TvgLogo = attrs["tvg-logo"]
	ch.GroupTitle = attrs["group-title"]
	ch.Number = attrs["tvg-chno"]
	if ch.Number == "" {
		ch.Number = attrs["channel-number"]
	}
	return ch
}

func parseAttributes(s string) map[string]string {
	attrs := make(map[string]string)
	i := 0
	n := len(s)
	for i < n {
		for i < n && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= n {
			break
		}
		// Read key up to '=' or whitespace.
		start := i
		for i < n && s[i] != '=' && s[i] != ' ' && s[i] != '\t' {
			i++
		}
		if i >= n || s[i] != '=' {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(s[start:i]))
		i++ // skip '='
		if i >= n {
			break
		}

		var val string
		if s[i] == '"' {
			i++
			valStart := i
			for i < n && s[i] != '"' {
				i++
			}
			val = s[valStart:i]
			if i < n && s[i] == '"' {
				i++
			}
		} else {
			valStart := i
			for i < n && s[i] != ' ' && s[i] != '\t' {
				i++
			}
			val = s[valStart:i]
		}
		if key != "" {
			attrs[key] = val
		}
	}
	return attrs
}
