package playback

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
)

func TestAuditV0109_M34_M35_SubtitleMethodNormAndNoEmbyWebWASM(t *testing.T) {
	// M-34: Lowercase / mixed-case SubtitleProfile.Method is normalized to canonical casing.
	prof := &DeviceProfile{
		SubtitleProfiles: []SubtitleProfile{
			{Format: "srt", Method: "external"},
			{Format: "ass", Method: "encode"},
			{Format: "vtt", Method: "EMBED"},
		},
	}
	if got := decideSubtitleDeliveryMethod(&dto.MediaStream{Codec: "srt"}, prof); got != "External" {
		t.Fatalf("expected External, got %q", got)
	}
	if got := decideSubtitleDeliveryMethod(&dto.MediaStream{Codec: "ass"}, prof); got != "Encode" {
		t.Fatalf("expected Encode, got %q", got)
	}
	if got := decideSubtitleDeliveryMethod(&dto.MediaStream{Codec: "vtt", IsExternal: false}, prof); got != "Embed" {
		t.Fatalf("expected Embed, got %q", got)
	}

	// M-35: "Emby Web" without SupportsClientWASM=true must NOT enable ClientWASM.
	if supportsClientWASM(&DeviceProfile{Name: "Emby Web"}) {
		t.Fatal("Emby Web must not trigger ClientWASM without SupportsClientWASM=true")
	}
	if !supportsClientWASM(&DeviceProfile{Name: "Emby Web", SupportsClientWASM: true}) {
		t.Fatal("explicit SupportsClientWASM=true should enable ClientWASM")
	}
	if !supportsClientWASM(&DeviceProfile{Name: "Ambra Web"}) {
		t.Fatal("Ambra Web should still enable ClientWASM")
	}
}

func TestAuditV0109_M40_NonChineseLangAndTitleSuffixIsolation(t *testing.T) {
	// Explicit non-Chinese language returns "" even if title/path contains chs/sc/tc tokens.
	if got := detectSubChineseVariant(dto.MediaStream{
		Language: "eng",
		Title:    "English SC",
		Path:     "/media/Movie.SC.2024.eng.srt",
	}); got != "" {
		t.Fatalf("expected empty variant for explicit eng track, got %q", got)
	}
	if got := detectSubChineseVariant(dto.MediaStream{
		Language: "jpn",
		Title:    "Japanese TC",
		Path:     "/media/Anime.TC.jpn.ass",
	}); got != "" {
		t.Fatalf("expected empty variant for explicit jpn track, got %q", got)
	}

	// Video title containing "SC" or "GB" must not trigger Chinese variant when suffix has no Chinese marker.
	if got := detectSubChineseVariant(dto.MediaStream{
		Path: "/media/StarCraft.SC.2024.1080p.mkv.srt",
	}); got != "" {
		t.Fatalf("expected empty variant when SC is only in video title prefix, got %q", got)
	}

	// Actual subtitle suffix still detects chs/cht properly.
	if got := detectSubChineseVariant(dto.MediaStream{
		Path: "/media/StarCraft.2024.sc.srt",
	}); got != "chs" {
		t.Fatalf("expected chs for .sc.srt suffix, got %q", got)
	}
	if got := detectSubChineseVariant(dto.MediaStream{
		Path: "/media/StarCraft.2024.tc.forced.ass",
	}); got != "cht" {
		t.Fatalf("expected cht for .tc.forced.ass suffix, got %q", got)
	}
}

func TestAuditV0109_L24_SubtitleParserEdgeCases(t *testing.T) {
	// 1. Standalone \r and whitespace-only blank lines in SRT
	srtContent := "1\r00:00:01,000 --> 00:00:02,500\rLine A\r   \t  \r2\r00:00:03,000 --> 00:00:04,000\rLine B\r"
	events := parseSRTOrVTT(srtContent, 0)
	if len(events) != 2 || events[0].Text != "Line A" || events[1].Text != "Line B" {
		t.Fatalf("unexpected SRT parsed events: %+v", events)
	}

	// 2. ASS/SSA with custom [Events] Format: column ordering (9-column SSA format)
	ssa := "[Script Info]\nTitle: SSA\n[Events]\nFormat: Marked, Start, End, Style, Name, MarginL, MarginR, MarginV, Text\nDialogue: Marked=0,0:00:01.5,0:00:03.250,Default,,0,0,0,Hello, SSA world!\n"
	tmp := t.TempDir()
	ssaPath := filepath.Join(tmp, "test.ssa")
	if err := os.WriteFile(ssaPath, []byte(ssa), 0o600); err != nil {
		t.Fatal(err)
	}
	assEvents, err := parseASS(ssa, 0)
	if err != nil {
		t.Fatalf("parseASS: %v", err)
	}
	if len(assEvents) != 1 {
		t.Fatalf("expected 1 SSA event, got %+v", assEvents)
	}
	if assEvents[0].Text != "Hello, SSA world!" {
		t.Fatalf("unexpected SSA text: %q", assEvents[0].Text)
	}
	// 0:00:01.5 -> 1500ms = 15_000_000 ticks; 0:00:03.250 -> 3250ms = 32_500_000 ticks
	if assEvents[0].StartPositionTicks != 15_000_000 || assEvents[0].EndPositionTicks != 32_500_000 {
		t.Fatalf("unexpected normalized ASS timestamps: start=%d end=%d", assEvents[0].StartPositionTicks, assEvents[0].EndPositionTicks)
	}

	// 3. Direct parseASSTimeMs fractional digit normalization
	if got := parseASSTimeMs("0:00:01.5"); got != 1500 {
		t.Fatalf("parseASSTimeMs(.5) = %d, want 1500", got)
	}
	if got := parseASSTimeMs("0:00:01.05"); got != 1050 {
		t.Fatalf("parseASSTimeMs(.05) = %d, want 1050", got)
	}
	if got := parseASSTimeMs("0:00:01.005"); got != 1005 {
		t.Fatalf("parseASSTimeMs(.005) = %d, want 1005", got)
	}
}
