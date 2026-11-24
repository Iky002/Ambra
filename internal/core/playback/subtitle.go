package playback

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// SubtitleTrackEvent matches Emby's .js subtitle JSON format.
type SubtitleTrackEvent struct {
	ID                 string `json:"Id"`
	Text               string `json:"Text"`
	StartPositionTicks int64  `json:"StartPositionTicks"`
	EndPositionTicks   int64  `json:"EndPositionTicks"`
}

type SubtitleTrackInfo struct {
	TrackEvents []SubtitleTrackEvent `json:"TrackEvents"`
}

var (
	reSRTTime = regexp.MustCompile(`(\d{1,2}):(\d{2}):(\d{2})[,.](\d{3})\s*-->\s*(\d{1,2}):(\d{2}):(\d{2})[,.](\d{3})`)
	reASSTags = regexp.MustCompile(`\{[^}]*\}`)
)

// ServeSubtitle loads a subtitle file from disk and converts it on the fly if targetFormat differs.
// Supports passthrough (.ass/.ssa/.sup/.srt/.vtt), SRT/ASS -> WebVTT, and Emby .js JSON.
func ServeSubtitle(filePath, targetFormat string, startPositionTicks int64) (string, []byte, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", nil, err
	}

	srcExt := strings.ToLower(strings.TrimPrefix(filepath.Ext(filePath), "."))
	dst := strings.ToLower(strings.TrimPrefix(targetFormat, "."))
	if dst == "" {
		dst = srcExt
	}

	// Passthrough when source and target formats match (and no time offset requested)
	if dst == srcExt && startPositionTicks == 0 {
		return subtitleContentType(dst), raw, nil
	}
	// Binary PGS (.sup) is always served verbatim
	if srcExt == "sup" || dst == "sup" || dst == "pgssub" {
		return "application/octet-stream", raw, nil
	}
	// Passthrough ASS/SSA verbatim
	if (srcExt == "ass" || srcExt == "ssa") && (dst == "ass" || dst == "ssa") {
		return subtitleContentType(dst), raw, nil
	}

	// Parse cues from SRT / VTT / ASS for conversion to VTT or Emby .js
	var events []SubtitleTrackEvent
	switch srcExt {
	case "srt", "vtt":
		events = parseSRTOrVTT(string(raw), startPositionTicks)
	case "ass", "ssa":
		events = parseASS(string(raw), startPositionTicks)
	default:
		return subtitleContentType(srcExt), raw, nil
	}

	switch dst {
	case "vtt", "webvtt":
		return "text/vtt; charset=utf-8", []byte(renderWebVTT(events)), nil
	case "js", "json":
		data, err := json.Marshal(SubtitleTrackInfo{TrackEvents: events})
		return "application/json; charset=utf-8", data, err
	default:
		return subtitleContentType(srcExt), raw, nil
	}
}

func subtitleContentType(ext string) string {
	switch strings.ToLower(ext) {
	case "vtt", "webvtt":
		return "text/vtt; charset=utf-8"
	case "srt":
		return "application/x-subrip; charset=utf-8"
	case "ass", "ssa":
		return "text/x-ssa; charset=utf-8"
	case "js", "json":
		return "application/json; charset=utf-8"
	case "sup", "pgssub":
		return "application/octet-stream"
	default:
		return "text/plain; charset=utf-8"
	}
}

func parseSRTOrVTT(content string, offsetTicks int64) []SubtitleTrackEvent {
	content = strings.TrimPrefix(content, "\xef\xbb\xbf")
	content = strings.ReplaceAll(content, "\r\n", "\n")
	blocks := strings.Split(content, "\n\n")

	var events []SubtitleTrackEvent
	seq := 1
	for _, block := range blocks {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		if len(lines) < 2 {
			continue
		}
		timeLineIdx := -1
		var startMs, endMs int64
		for i, line := range lines {
			if m := reSRTTime.FindStringSubmatch(line); m != nil {
				startMs = hmsmsToMs(m[1], m[2], m[3], m[4])
				endMs = hmsmsToMs(m[5], m[6], m[7], m[8])
				timeLineIdx = i
				break
			}
		}
		if timeLineIdx == -1 || timeLineIdx+1 >= len(lines) {
			continue
		}
		startTicks := startMs*10_000 - offsetTicks
		endTicks := endMs*10_000 - offsetTicks
		if endTicks <= 0 {
			continue
		}
		if startTicks < 0 {
			startTicks = 0
		}
		text := strings.Join(lines[timeLineIdx+1:], "\n")
		events = append(events, SubtitleTrackEvent{
			ID:                 strconv.Itoa(seq),
			Text:               text,
			StartPositionTicks: startTicks,
			EndPositionTicks:   endTicks,
		})
		seq++
	}
	return events
}

func parseASS(content string, offsetTicks int64) []SubtitleTrackEvent {
	content = strings.TrimPrefix(content, "\xef\xbb\xbf")
	sc := bufio.NewScanner(strings.NewReader(content))
	var events []SubtitleTrackEvent
	seq := 1
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "Dialogue:") {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(line, "Dialogue:"))
		// Standard ASS Dialogue has 10 comma-separated fields; Text is the 10th (index 9)
		parts := strings.SplitN(body, ",", 10)
		if len(parts) < 10 {
			continue
		}
		startMs := parseASSTimeMs(parts[1])
		endMs := parseASSTimeMs(parts[2])
		startTicks := startMs*10_000 - offsetTicks
		endTicks := endMs*10_000 - offsetTicks
		if endTicks <= 0 {
			continue
		}
		if startTicks < 0 {
			startTicks = 0
		}
		text := reASSTags.ReplaceAllString(parts[9], "")
		text = strings.ReplaceAll(text, `\N`, "\n")
		text = strings.ReplaceAll(text, `\n`, "\n")
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		events = append(events, SubtitleTrackEvent{
			ID:                 strconv.Itoa(seq),
			Text:               text,
			StartPositionTicks: startTicks,
			EndPositionTicks:   endTicks,
		})
		seq++
	}
	return events
}

func renderWebVTT(events []SubtitleTrackEvent) string {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for _, ev := range events {
		b.WriteString(formatVTTTime(ev.StartPositionTicks / 10_000))
		b.WriteString(" --> ")
		b.WriteString(formatVTTTime(ev.EndPositionTicks / 10_000))
		b.WriteString("\n")
		b.WriteString(ev.Text)
		b.WriteString("\n\n")
	}
	return b.String()
}

func hmsmsToMs(h, m, s, ms string) int64 {
	hh, _ := strconv.ParseInt(h, 10, 64)
	mm, _ := strconv.ParseInt(m, 10, 64)
	ss, _ := strconv.ParseInt(s, 10, 64)
	msVal, _ := strconv.ParseInt(ms, 10, 64)
	return ((hh*60+mm)*60+ss)*1000 + msVal
}

func parseASSTimeMs(s string) int64 {
	// Format: H:MM:SS.cc (centiseconds)
	s = strings.TrimSpace(s)
	dot := strings.IndexByte(s, '.')
	var cs int64
	hms := s
	if dot != -1 {
		cs, _ = strconv.ParseInt(s[dot+1:], 10, 64)
		hms = s[:dot]
	}
	parts := strings.Split(hms, ":")
	if len(parts) != 3 {
		return 0
	}
	hh, _ := strconv.ParseInt(parts[0], 10, 64)
	mm, _ := strconv.ParseInt(parts[1], 10, 64)
	ss, _ := strconv.ParseInt(parts[2], 10, 64)
	return ((hh*60+mm)*60+ss)*1000 + cs*10
}

func formatVTTTime(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	h := ms / 3600000
	ms %= 3600000
	m := ms / 60000
	ms %= 60000
	s := ms / 1000
	rem := ms % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, rem)
}
