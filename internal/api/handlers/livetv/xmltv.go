package livetv

import (
	"bufio"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// XMLTVChannel represents a <channel> definition from an XMLTV EPG feed.
type XMLTVChannel struct {
	ID           string
	DisplayNames []string
}

// XMLTVProgramme represents a <programme> entry from an XMLTV EPG feed.
type XMLTVProgramme struct {
	Channel    string
	Start      time.Time
	Stop       time.Time
	Title      string
	Desc       string
	Categories []string
}

// XMLTVGuide holds the parsed channels and programmes from an XMLTV feed.
type XMLTVGuide struct {
	Channels   []XMLTVChannel
	Programmes []XMLTVProgramme
}

const (
	maxXMLTVDecompressedBytes int64 = 100 << 20 // 100 MiB max decompressed XMLTV stream
	maxXMLTVProgrammes        int   = 200000
)

// ParseXMLTV parses an XMLTV EPG feed from r using streaming XML decoding.
// Both plain XML (.xml) and gzip-compressed XML (.xml.gz) streams are detected automatically.
func ParseXMLTV(r io.Reader) (*XMLTVGuide, error) {
	return parseXMLTVWithLimits(r, maxXMLTVDecompressedBytes, maxXMLTVProgrammes)
}

func parseXMLTVWithLimits(r io.Reader, maxBytes int64, maxProgrammes int) (*XMLTVGuide, error) {
	if r == nil {
		return nil, errors.New("xmltv reader is nil")
	}

	br := bufio.NewReader(r)
	var src io.Reader = br
	if hdr, err := br.Peek(2); err == nil && len(hdr) == 2 && hdr[0] == 0x1f && hdr[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("open gzip xmltv: %w", err)
		}
		defer gz.Close()
		src = gz
	}

	var limited *io.LimitedReader
	if maxBytes > 0 {
		limited = &io.LimitedReader{R: src, N: maxBytes + 1}
		src = limited
	}

	type rawChannel struct {
		ID           string   `xml:"id,attr"`
		DisplayNames []string `xml:"display-name"`
	}
	type rawProgramme struct {
		Start      string   `xml:"start,attr"`
		Stop       string   `xml:"stop,attr"`
		Channel    string   `xml:"channel,attr"`
		Titles     []string `xml:"title"`
		Descs      []string `xml:"desc"`
		Categories []string `xml:"category"`
	}

	guide := &XMLTVGuide{}
	dec := xml.NewDecoder(src)

	for {
		tok, err := dec.Token()
		if err != nil {
			if limited != nil && limited.N <= 0 {
				return nil, fmt.Errorf("xmltv decompressed size exceeds %d byte limit", maxBytes)
			}
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode xmltv: %w", err)
		}
		if limited != nil && limited.N <= 0 {
			return nil, fmt.Errorf("xmltv decompressed size exceeds %d byte limit", maxBytes)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "channel":
			var rc rawChannel
			if err := dec.DecodeElement(&rc, &se); err != nil {
				if limited != nil && limited.N <= 0 {
					return nil, fmt.Errorf("xmltv decompressed size exceeds %d byte limit", maxBytes)
				}
				return nil, fmt.Errorf("decode xmltv channel: %w", err)
			}
			id := strings.TrimSpace(rc.ID)
			if id == "" {
				continue
			}
			names := make([]string, 0, len(rc.DisplayNames))
			for _, n := range rc.DisplayNames {
				if trimmed := strings.TrimSpace(n); trimmed != "" {
					names = append(names, trimmed)
				}
			}
			guide.Channels = append(guide.Channels, XMLTVChannel{
				ID:           id,
				DisplayNames: names,
			})

		case "programme":
			if maxProgrammes > 0 && len(guide.Programmes) >= maxProgrammes {
				return guide, nil
			}
			var rp rawProgramme
			if err := dec.DecodeElement(&rp, &se); err != nil {
				if limited != nil && limited.N <= 0 {
					return nil, fmt.Errorf("xmltv decompressed size exceeds %d byte limit", maxBytes)
				}
				return nil, fmt.Errorf("decode xmltv programme: %w", err)
			}
			chID := strings.TrimSpace(rp.Channel)
			title := firstNonEmpty(rp.Titles)
			if chID == "" || title == "" {
				continue
			}
			start, err := ParseXMLTVTime(rp.Start)
			if err != nil {
				continue
			}
			var stop time.Time
			if strings.TrimSpace(rp.Stop) != "" {
				if parsedStop, err := ParseXMLTVTime(rp.Stop); err == nil {
					stop = parsedStop
				}
			}
			if stop.IsZero() || !stop.After(start) {
				stop = start.Add(time.Hour)
			}
			cats := make([]string, 0, len(rp.Categories))
			for _, c := range rp.Categories {
				if trimmed := strings.TrimSpace(c); trimmed != "" {
					cats = append(cats, trimmed)
				}
			}
			guide.Programmes = append(guide.Programmes, XMLTVProgramme{
				Channel:    chID,
				Start:      start,
				Stop:       stop,
				Title:      title,
				Desc:       firstNonEmpty(rp.Descs),
				Categories: cats,
			})
		}
	}

	if limited != nil && limited.N <= 0 {
		return nil, fmt.Errorf("xmltv decompressed size exceeds %d byte limit", maxBytes)
	}

	return guide, nil
}

// ParseXMLTVTime parses standard XMLTV timestamps such as "20060102150405 -0700" or "20060102150405".
func ParseXMLTVTime(raw string) (time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, errors.New("empty xmltv timestamp")
	}
	layouts := []string{
		"20060102150405 -0700",
		"20060102150405 -07:00",
		"20060102150405-0700",
		"20060102150405",
		"200601021504 -0700",
		"200601021504",
		time.RFC3339,
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported xmltv timestamp %q", raw)
}

func firstNonEmpty(vals []string) string {
	for _, v := range vals {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func hasXMLTVExt(path string) bool {
	lower := strings.ToLower(strings.TrimSpace(path))
	return strings.HasSuffix(lower, ".xml") || strings.HasSuffix(lower, ".xml.gz")
}

func classifyXMLTVCategories(cats []string) (isMovie bool, isSeries bool) {
	for _, c := range cats {
		lower := strings.ToLower(c)
		if strings.Contains(lower, "movie") || strings.Contains(lower, "film") || strings.Contains(c, "电影") {
			isMovie = true
		}
		if strings.Contains(lower, "series") || strings.Contains(lower, "episode") || strings.Contains(lower, "tv show") || strings.Contains(lower, "drama") || strings.Contains(c, "剧") {
			isSeries = true
		}
	}
	return isMovie, isSeries
}
