package playback

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	maxSubtitleFileSize = 16 << 20 // 16 MiB
	maxSubtitleLineSize = 4 * 1024 * 1024
)

func readLimitedSubtitleFile(filePath string) ([]byte, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > maxSubtitleFileSize {
		return nil, fmt.Errorf("subtitle file exceeds maximum size of %d bytes", maxSubtitleFileSize)
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxSubtitleFileSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxSubtitleFileSize {
		return nil, fmt.Errorf("subtitle file exceeds maximum size of %d bytes", maxSubtitleFileSize)
	}
	return raw, nil
}

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
	// Matches both HH:MM:SS[.,]mmm and short WebVTT MM:SS[.,]mmm timestamps.
	reSRTTime = regexp.MustCompile(`(?:(\d{1,2}):)?(\d{2}):(\d{2})[,.](\d{3})\s*-->\s*(?:(\d{1,2}):)?(\d{2}):(\d{2})[,.](\d{3})`)
	reASSTags = regexp.MustCompile(`\{[^}]*\}`)
)

// ServeSubtitle loads a subtitle file from disk and converts it on the fly if targetFormat differs.
// Supports passthrough (.ass/.ssa/.sup/.srt/.vtt), SRT/ASS -> WebVTT, and Emby .js JSON.
func ServeSubtitle(filePath, targetFormat string, startPositionTicks int64) (string, []byte, error) {
	raw, err := readLimitedSubtitleFile(filePath)
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
	// ASS/SSA -> ASS/SSA: passthrough if no offset, else shift Dialogue timestamps in-place
	if (srcExt == "ass" || srcExt == "ssa") && (dst == "ass" || dst == "ssa") {
		if startPositionTicks <= 0 {
			return subtitleContentType(dst), raw, nil
		}
		shifted, err := shiftASS(string(raw), startPositionTicks)
		if err != nil {
			return "", nil, err
		}
		return subtitleContentType(dst), []byte(shifted), nil
	}

	// Parse cues from SRT / VTT / ASS for conversion to VTT, SRT, or Emby .js
	var events []SubtitleTrackEvent
	switch srcExt {
	case "srt", "vtt":
		events = parseSRTOrVTT(string(raw), startPositionTicks)
	case "ass", "ssa":
		events, err = parseASS(string(raw), startPositionTicks)
		if err != nil {
			return "", nil, err
		}
	default:
		return subtitleContentType(srcExt), raw, nil
	}

	switch dst {
	case "vtt", "webvtt":
		return "text/vtt; charset=utf-8", []byte(renderWebVTT(events)), nil
	case "srt", "subrip":
		return "application/x-subrip; charset=utf-8", []byte(renderSRT(events)), nil
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
	content = strings.ReplaceAll(content, "\r", "\n")

	var events []SubtitleTrackEvent
	seq := 1
	inCue := false
	var startMs, endMs int64
	var textLines []string

	flushCue := func() {
		if !inCue {
			return
		}
		inCue = false
		if len(textLines) == 0 {
			return
		}
		startTicks := startMs*10_000 - offsetTicks
		endTicks := endMs*10_000 - offsetTicks
		text := strings.Join(textLines, "\n")
		textLines = textLines[:0]
		if endTicks <= 0 {
			return
		}
		if startTicks < 0 {
			startTicks = 0
		}
		events = append(events, SubtitleTrackEvent{
			ID:                 strconv.Itoa(seq),
			Text:               text,
			StartPositionTicks: startTicks,
			EndPositionTicks:   endTicks,
		})
		seq++
	}

	for _, rawLine := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" {
			flushCue()
			continue
		}
		if !inCue {
			if m := reSRTTime.FindStringSubmatch(trimmed); m != nil {
				startMs = hmsmsToMs(m[1], m[2], m[3], m[4])
				endMs = hmsmsToMs(m[5], m[6], m[7], m[8])
				inCue = true
				textLines = textLines[:0]
			}
			continue
		}
		// If a malformed file omits a blank line between cues and starts a new timestamp directly:
		if m := reSRTTime.FindStringSubmatch(trimmed); m != nil {
			flushCue()
			startMs = hmsmsToMs(m[1], m[2], m[3], m[4])
			endMs = hmsmsToMs(m[5], m[6], m[7], m[8])
			inCue = true
			textLines = textLines[:0]
			continue
		}
		textLines = append(textLines, rawLine)
	}
	flushCue()
	return events
}

type assEventFormat struct {
	startIdx   int
	endIdx     int
	textIdx    int
	fieldCount int
}

func defaultASSEventFormat() assEventFormat {
	return assEventFormat{startIdx: 1, endIdx: 2, textIdx: 9, fieldCount: 10}
}

func parseASSEventFormat(line string) (assEventFormat, bool) {
	body := strings.TrimSpace(strings.TrimPrefix(line, "Format:"))
	if body == "" {
		return assEventFormat{}, false
	}
	cols := strings.Split(body, ",")
	fmtSpec := assEventFormat{startIdx: -1, endIdx: -1, textIdx: -1, fieldCount: len(cols)}
	for i, c := range cols {
		switch strings.ToLower(strings.TrimSpace(c)) {
		case "start":
			fmtSpec.startIdx = i
		case "end":
			fmtSpec.endIdx = i
		case "text":
			fmtSpec.textIdx = i
		}
	}
	if fmtSpec.startIdx < 0 || fmtSpec.endIdx < 0 || fmtSpec.textIdx < 0 {
		return assEventFormat{}, false
	}
	return fmtSpec, true
}

func parseASS(content string, offsetTicks int64) ([]SubtitleTrackEvent, error) {
	content = strings.TrimPrefix(content, "\xef\xbb\xbf")
	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 0, 64*1024), maxSubtitleLineSize)
	var events []SubtitleTrackEvent
	seq := 1
	inEvents := false
	evFmt := defaultASSEventFormat()

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inEvents = strings.EqualFold(line, "[Events]")
			continue
		}
		if inEvents && strings.HasPrefix(line, "Format:") {
			if parsed, ok := parseASSEventFormat(line); ok {
				evFmt = parsed
			}
			continue
		}
		if !strings.HasPrefix(line, "Dialogue:") {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(line, "Dialogue:"))
		parts := strings.SplitN(body, ",", evFmt.fieldCount)
		if len(parts) < evFmt.fieldCount {
			continue
		}
		startMs := parseASSTimeMs(parts[evFmt.startIdx])
		endMs := parseASSTimeMs(parts[evFmt.endIdx])
		startTicks := startMs*10_000 - offsetTicks
		endTicks := endMs*10_000 - offsetTicks
		if endTicks <= 0 {
			continue
		}
		if startTicks < 0 {
			startTicks = 0
		}
		text := reASSTags.ReplaceAllString(parts[evFmt.textIdx], "")
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
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan ASS subtitle: %w", err)
	}
	return events, nil
}

func shiftASS(content string, offsetTicks int64) (string, error) {
	hasBOM := strings.HasPrefix(content, "\xef\xbb\xbf")
	content = strings.TrimPrefix(content, "\xef\xbb\xbf")
	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 0, 64*1024), maxSubtitleLineSize)
	var b strings.Builder
	if hasBOM {
		b.WriteString("\xef\xbb\xbf")
	}
	inEvents := false
	evFmt := defaultASSEventFormat()
	for sc.Scan() {
		rawLine := sc.Text()
		trimmed := strings.TrimSpace(rawLine)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inEvents = strings.EqualFold(trimmed, "[Events]")
			b.WriteString(rawLine)
			b.WriteByte('\n')
			continue
		}
		if inEvents && strings.HasPrefix(trimmed, "Format:") {
			if parsed, ok := parseASSEventFormat(trimmed); ok {
				evFmt = parsed
			}
			b.WriteString(rawLine)
			b.WriteByte('\n')
			continue
		}
		if !strings.HasPrefix(trimmed, "Dialogue:") {
			b.WriteString(rawLine)
			b.WriteByte('\n')
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(trimmed, "Dialogue:"))
		parts := strings.SplitN(body, ",", evFmt.fieldCount)
		if len(parts) < evFmt.fieldCount {
			b.WriteString(rawLine)
			b.WriteByte('\n')
			continue
		}
		startMs := parseASSTimeMs(parts[evFmt.startIdx])
		endMs := parseASSTimeMs(parts[evFmt.endIdx])
		startTicks := startMs*10_000 - offsetTicks
		endTicks := endMs*10_000 - offsetTicks
		if endTicks <= 0 {
			continue
		}
		if startTicks < 0 {
			startTicks = 0
		}
		parts[evFmt.startIdx] = formatASSTime(startTicks / 10_000)
		parts[evFmt.endIdx] = formatASSTime(endTicks / 10_000)
		b.WriteString("Dialogue: ")
		b.WriteString(strings.Join(parts, ","))
		b.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("scan ASS subtitle: %w", err)
	}
	return b.String(), nil
}

func formatASSTime(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	cs := (ms % 1000) / 10
	sec := (ms / 1000) % 60
	min := (ms / 60000) % 60
	hour := ms / 3600000
	return fmt.Sprintf("%d:%02d:%02d.%02d", hour, min, sec, cs)
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

func renderSRT(events []SubtitleTrackEvent) string {
	var b strings.Builder
	for i, ev := range events {
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteByte('\n')
		b.WriteString(formatSRTTime(ev.StartPositionTicks / 10_000))
		b.WriteString(" --> ")
		b.WriteString(formatSRTTime(ev.EndPositionTicks / 10_000))
		b.WriteByte('\n')
		b.WriteString(ev.Text)
		b.WriteString("\n\n")
	}
	return b.String()
}

func formatSRTTime(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	h := ms / 3600000
	ms %= 3600000
	m := ms / 60000
	ms %= 60000
	s := ms / 1000
	rem := ms % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, rem)
}

func hmsmsToMs(h, m, s, ms string) int64 {
	hh, _ := strconv.ParseInt(h, 10, 64)
	mm, _ := strconv.ParseInt(m, 10, 64)
	ss, _ := strconv.ParseInt(s, 10, 64)
	msVal, _ := strconv.ParseInt(ms, 10, 64)
	return ((hh*60+mm)*60+ss)*1000 + msVal
}

func parseASSTimeMs(s string) int64 {
	// Format: H:MM:SS.c / H:MM:SS.cc / H:MM:SS.mmm
	s = strings.TrimSpace(s)
	dot := strings.IndexAny(s, ".,")
	var fracMs int64
	hms := s
	if dot != -1 {
		fracStr := strings.TrimSpace(s[dot+1:])
		hms = s[:dot]
		if len(fracStr) > 3 {
			fracStr = fracStr[:3]
		}
		if v, err := strconv.ParseInt(fracStr, 10, 64); err == nil && v > 0 {
			switch len(fracStr) {
			case 1:
				fracMs = v * 100
			case 2:
				fracMs = v * 10
			default:
				fracMs = v
			}
		}
	}
	parts := strings.Split(hms, ":")
	if len(parts) != 3 {
		return 0
	}
	hh, _ := strconv.ParseInt(parts[0], 10, 64)
	mm, _ := strconv.ParseInt(parts[1], 10, 64)
	ss, _ := strconv.ParseInt(parts[2], 10, 64)
	return ((hh*60+mm)*60+ss)*1000 + fracMs
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
