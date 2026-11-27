package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/auth"
	"github.com/ambra-media/ambra/internal/core/transcode"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

type audioRoundTripFunc func(*http.Request) (*http.Response, error)

func (f audioRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func makeSyncsafe32(n int) [4]byte {
	return [4]byte{
		byte((n >> 21) & 0x7F),
		byte((n >> 14) & 0x7F),
		byte((n >> 7) & 0x7F),
		byte(n & 0x7F),
	}
}

func encodeUTF16Bytes(s string, bigEndian bool, withBOM bool) []byte {
	codes := utf16.Encode([]rune(s))
	var buf bytes.Buffer
	if withBOM {
		if bigEndian {
			buf.Write([]byte{0xFE, 0xFF})
		} else {
			buf.Write([]byte{0xFF, 0xFE})
		}
	}
	for _, c := range codes {
		var b [2]byte
		if bigEndian {
			binary.BigEndian.PutUint16(b[:], c)
		} else {
			binary.LittleEndian.PutUint16(b[:], c)
		}
		buf.Write(b[:])
	}
	return buf.Bytes()
}

func buildMP4Box(boxType string, payload []byte) []byte {
	var buf bytes.Buffer
	var hdr [8]byte
	binary.BigEndian.PutUint32(hdr[0:4], uint32(8+len(payload)))
	copy(hdr[4:8], boxType)
	buf.Write(hdr[:])
	buf.Write(payload)
	return buf.Bytes()
}

func buildMP4Box64(boxType string, payload []byte) []byte {
	var buf bytes.Buffer
	var hdr [16]byte
	binary.BigEndian.PutUint32(hdr[0:4], 1)
	copy(hdr[4:8], boxType)
	binary.BigEndian.PutUint64(hdr[8:16], uint64(16+len(payload)))
	buf.Write(hdr[:])
	buf.Write(payload)
	return buf.Bytes()
}

func createFakeFFmpegScript(t *testing.T, output string, exitCode int) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), "fake-ffmpeg.sh")
	var content string
	if exitCode != 0 {
		content = "#!/bin/sh\nexit " + string(rune('0'+exitCode)) + "\n"
	} else {
		content = "#!/bin/sh\nprintf '%s' '" + output + "'\n"
	}
	if err := os.WriteFile(scriptPath, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake ffmpeg: %v", err)
	}
	return scriptPath
}

func TestV0110_EmbeddedLyricsAllFormatsAndEncodings(t *testing.T) {
	tmp := t.TempDir()

	// 1. Edge cases on ExtractEmbeddedLyrics: missing file, directory, tiny file, unknown magic
	if _, err := ExtractEmbeddedLyrics(filepath.Join(tmp, "nonexistent.mp3")); err == nil {
		t.Fatalf("expected error for nonexistent file")
	}
	if got, err := ExtractEmbeddedLyrics(tmp); err != nil || got != "" {
		t.Fatalf("expected empty for dir, got %q err=%v", got, err)
	}
	tinyPath := filepath.Join(tmp, "tiny.bin")
	_ = os.WriteFile(tinyPath, []byte("abc"), 0o644)
	if got, err := ExtractEmbeddedLyrics(tinyPath); err != nil || got != "" {
		t.Fatalf("expected empty for <8B file, got %q err=%v", got, err)
	}
	unkPath := filepath.Join(tmp, "unknown.bin")
	_ = os.WriteFile(unkPath, []byte("1234567890abcdef"), 0o644)
	if got, err := ExtractEmbeddedLyrics(unkPath); err != nil || got != "" {
		t.Fatalf("expected empty for unknown magic, got %q err=%v", got, err)
	}

	// Unit helper edge cases
	if syncsafeToInt([]byte{1, 2}) != 0 {
		t.Fatalf("expected 0 for short syncsafe slice")
	}
	plainBytes := []byte{0x01, 0xFF, 0x02}
	if !bytes.Equal(removeID3Unsynchronisation(plainBytes), plainBytes) {
		t.Fatalf("expected unchanged slice when no 0xFF 0x00 present")
	}
	unsyncedBytes := []byte{0xFF, 0x00, 0xE0, 0x41, 0xFF, 0x00, 0x00}
	if got := removeID3Unsynchronisation(unsyncedBytes); !bytes.Equal(got, []byte{0xFF, 0xE0, 0x41, 0xFF, 0x00}) {
		t.Fatalf("unexpected unsync removal: %v", got)
	}
	if decodeID3Text(nil, 0) != "" {
		t.Fatalf("expected empty decodeID3Text on nil")
	}
	if h, r := splitID3NullTerminated([]byte("abc"), 3); string(h) != "abc" || r != nil {
		t.Fatalf("expected full head when no null terminator")
	}
	if h, r := splitID3NullTerminated([]byte{0x00, 0x41, 0x00, 0x42}, 2); len(h) != 4 || r != nil {
		t.Fatalf("expected full head when UTF-16 has no double-null terminator")
	}
	if parseID3USLT([]byte{0, 'e', 'n'}) != "" {
		t.Fatalf("expected empty USLT on short payload")
	}
	if k, v := parseID3TXXX([]byte{0}); k != "" || v != "" {
		t.Fatalf("expected empty TXXX on short payload")
	}
	if parseID3SYLT([]byte{0, 'e', 'n', 'g', 1, 1, 0}) != "" {
		t.Fatalf("expected empty SYLT when tsFormat != 2")
	}
	if parseID3SYLT([]byte{0, 'e', 'n'}) != "" {
		t.Fatalf("expected empty SYLT on short payload")
	}
	if l, u := parseVorbisCommentLyrics([]byte{1, 2, 3}); l != "" || u != "" {
		t.Fatalf("expected empty Vorbis comments on short slice")
	}

	// 2. FLAC Vorbis Comment:
	//    (a) STREAMINFO block + VORBIS_COMMENT with SYNCEDLYRICS overriding UNSYNCEDLYRICS
	buildVorbisBlock := func(entries []string) []byte {
		var b bytes.Buffer
		vendor := "AmbraTest"
		_ = binary.Write(&b, binary.LittleEndian, uint32(len(vendor)))
		b.WriteString(vendor)
		_ = binary.Write(&b, binary.LittleEndian, uint32(len(entries)))
		for _, e := range entries {
			_ = binary.Write(&b, binary.LittleEndian, uint32(len(e)))
			b.WriteString(e)
		}
		return b.Bytes()
	}
	writeFLAC := func(name string, blocks []struct {
		bType  byte
		isLast bool
		data   []byte
	}) string {
		var b bytes.Buffer
		b.WriteString("fLaC")
		for _, blk := range blocks {
			hdr0 := blk.bType & 0x7F
			if blk.isLast {
				hdr0 |= 0x80
			}
			l := len(blk.data)
			b.Write([]byte{hdr0, byte(l >> 16), byte(l >> 8), byte(l)})
			b.Write(blk.data)
		}
		p := filepath.Join(tmp, name)
		if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	flacSynced := writeFLAC("synced.flac", []struct {
		bType  byte
		isLast bool
		data   []byte
	}{
		{bType: 0, isLast: false, data: make([]byte, 34)},
		{bType: 4, isLast: true, data: buildVorbisBlock([]string{
			"INVALID_NO_EQUALS",
			"LYRICS=   ",
			"UNSYNCEDLYRICS=Fallback line",
			"SYNCEDLYRICS=[00:04.20]FLAC Synced Line",
		})},
	})
	gotFLAC, err := ExtractEmbeddedLyrics(flacSynced)
	if err != nil || gotFLAC != "[00:04.20]FLAC Synced Line" {
		t.Fatalf("FLAC synced lyrics mismatch: %q, err=%v", gotFLAC, err)
	}

	flacUnsynced := writeFLAC("unsynced.flac", []struct {
		bType  byte
		isLast bool
		data   []byte
	}{
		{bType: 4, isLast: true, data: buildVorbisBlock([]string{
			"UNSYNCED LYRICS=Plain FLAC Poetry",
		})},
	})
	gotFLACUnsynced, err := ExtractEmbeddedLyrics(flacUnsynced)
	if err != nil || gotFLACUnsynced != "Plain FLAC Poetry" {
		t.Fatalf("FLAC unsynced fallback mismatch: %q, err=%v", gotFLACUnsynced, err)
	}

	// 3. MP3 ID3v2.4 with Unsync tag flag (0x80), Extended Header (0x40),
	//    frame data-length indicator (0x01) + frame unsync (0x02), and UTF-16LE BOM SYLT
	var syltPayload bytes.Buffer
	// 4B data length indicator before SYLT body
	_ = binary.Write(&syltPayload, binary.BigEndian, uint32(64))
	syltPayload.WriteByte(1) // UTF-16 with BOM
	syltPayload.WriteString("eng")
	syltPayload.WriteByte(2) // ms timestamps
	syltPayload.WriteByte(1) // lyrics content
	syltPayload.Write([]byte{0x00, 0x00}) // empty UTF-16 descriptor terminator
	// Cue 1: "Hello\nWorld" at 2500ms
	syltPayload.Write(encodeUTF16Bytes("Hello\nWorld", false, true))
	syltPayload.Write([]byte{0x00, 0x00})
	_ = binary.Write(&syltPayload, binary.BigEndian, uint32(2500))
	// Cue 2: "Second" (UTF-16BE BOM) at 65123ms
	syltPayload.Write(encodeUTF16Bytes("Second", true, true))
	syltPayload.Write([]byte{0x00, 0x00})
	_ = binary.Write(&syltPayload, binary.BigEndian, uint32(65123))

	// Apply ID3 unsynchronisation (0xFF -> 0xFF 0x00) to frame payload to test frame flag 0x02
	var unsyncedFrame bytes.Buffer
	for _, b := range syltPayload.Bytes() {
		unsyncedFrame.WriteByte(b)
		if b == 0xFF {
			unsyncedFrame.WriteByte(0x00)
		}
	}

	var id3v24Body bytes.Buffer
	// v2.4 Extended header (syncsafe size = 6, includes the 4 size bytes)
	extHdr24 := makeSyncsafe32(6)
	id3v24Body.Write(extHdr24[:])
	id3v24Body.Write([]byte{0x00, 0x00})
	// SYLT frame header (v2.4 uses syncsafe frame size)
	id3v24Body.WriteString("SYLT")
	fSize24 := makeSyncsafe32(unsyncedFrame.Len())
	id3v24Body.Write(fSize24[:])
	id3v24Body.Write([]byte{0x00, 0x03}) // frameFlags2: 0x01 (data len) | 0x02 (unsync)
	id3v24Body.Write(unsyncedFrame.Bytes())

	var mp3v24 bytes.Buffer
	mp3v24.WriteString("ID3")
	mp3v24.Write([]byte{4, 0, 0xC0}) // v2.4, flags: 0x80 (unsync) | 0x40 (ext header)
	var tagUnsynced bytes.Buffer
	for _, b := range id3v24Body.Bytes() {
		tagUnsynced.WriteByte(b)
		if b == 0xFF {
			tagUnsynced.WriteByte(0x00)
		}
	}
	tagSz24 := makeSyncsafe32(tagUnsynced.Len())
	mp3v24.Write(tagSz24[:])
	mp3v24.Write(tagUnsynced.Bytes())
	mp3v24Path := filepath.Join(tmp, "sylt_v24.mp3")
	_ = os.WriteFile(mp3v24Path, mp3v24.Bytes(), 0o644)

	gotSYLT, err := ExtractEmbeddedLyrics(mp3v24Path)
	if err != nil || !strings.Contains(gotSYLT, "[00:02.500]Hello World") || !strings.Contains(gotSYLT, "[01:05.123]Second") {
		t.Fatalf("ID3v2.4 SYLT mismatch: %q, err=%v", gotSYLT, err)
	}

	// 4. MP3 ID3v2.3 with Extended Header (0x40) + USLT (UTF-16BE enc=2 synced) & ISO-8859-1 unsynced + TXXX UTF-8
	buildID3v23MP3 := func(name string, frames []struct {
		id   string
		body []byte
	}) string {
		var body bytes.Buffer
		// v2.3 extended header: 4B size (6) + 6B payload
		_ = binary.Write(&body, binary.BigEndian, uint32(6))
		body.Write(make([]byte, 6))
		for _, fr := range frames {
			body.WriteString(fr.id)
			_ = binary.Write(&body, binary.BigEndian, uint32(len(fr.body)))
			body.Write([]byte{0, 0})
			body.Write(fr.body)
		}
		var out bytes.Buffer
		out.WriteString("ID3")
		out.Write([]byte{3, 0, 0x40}) // v2.3, ext header
		sz := makeSyncsafe32(body.Len())
		out.Write(sz[:])
		out.Write(body.Bytes())
		p := filepath.Join(tmp, name)
		_ = os.WriteFile(p, out.Bytes(), 0o644)
		return p
	}

	// 4a. USLT UTF-16BE (enc=2) with [mm:ss] timestamp
	var usltUTF16BE bytes.Buffer
	usltUTF16BE.WriteByte(2) // UTF-16BE without BOM
	usltUTF16BE.WriteString("jpn")
	usltUTF16BE.Write(encodeUTF16Bytes("desc", true, false))
	usltUTF16BE.Write([]byte{0x00, 0x00})
	usltUTF16BE.Write(encodeUTF16Bytes("[00:08.00]UTF16BE Line", true, false))
	mp3USLT16 := buildID3v23MP3("uslt_utf16be.mp3", []struct {
		id   string
		body []byte
	}{{id: "USLT", body: usltUTF16BE.Bytes()}})
	if got, err := ExtractEmbeddedLyrics(mp3USLT16); err != nil || got != "[00:08.00]UTF16BE Line" {
		t.Fatalf("ID3v2.3 USLT UTF-16BE mismatch: %q, err=%v", got, err)
	}

	// 4b. TXXX (UTF-8 enc=3, key="LYRICS") + ISO-8859-1 USLT
	var txxxBody bytes.Buffer
	txxxBody.WriteByte(3) // UTF-8
	txxxBody.WriteString("LYRICS\x00TXXX Embedded Verse")
	mp3TXXX := buildID3v23MP3("txxx_utf8.mp3", []struct {
		id   string
		body []byte
	}{{id: "TXXX", body: txxxBody.Bytes()}})
	if got, err := ExtractEmbeddedLyrics(mp3TXXX); err != nil || got != "TXXX Embedded Verse" {
		t.Fatalf("ID3v2.3 TXXX mismatch: %q, err=%v", got, err)
	}

	// 5. MP3 ID3v2.2 (majorVer=2): 3-byte ULT (ISO-8859-1 enc=0) and SLT (UTF-8 enc=3)
	buildID3v22MP3 := func(name, frameID string, payload []byte) string {
		var body bytes.Buffer
		body.WriteString(frameID)
		l := len(payload)
		body.Write([]byte{byte(l >> 16), byte(l >> 8), byte(l)})
		body.Write(payload)
		body.Write([]byte{0, 0, 0, 0, 0, 0}) // trailing padding triggers buf[pos]==0 break
		var out bytes.Buffer
		out.WriteString("ID3")
		out.Write([]byte{2, 0, 0})
		sz := makeSyncsafe32(body.Len())
		out.Write(sz[:])
		out.Write(body.Bytes())
		p := filepath.Join(tmp, name)
		_ = os.WriteFile(p, out.Bytes(), 0o644)
		return p
	}

	var ultPayload bytes.Buffer
	ultPayload.WriteByte(0) // ISO-8859-1
	ultPayload.WriteString("eng")
	ultPayload.WriteString("d\x00Caf\xe9 Lyrics") // 0xE9 = 'é' in ISO-8859-1
	mp3v22ULT := buildID3v22MP3("v22_ult.mp3", "ULT", ultPayload.Bytes())
	if got, err := ExtractEmbeddedLyrics(mp3v22ULT); err != nil || got != "Café Lyrics" {
		t.Fatalf("ID3v2.2 ULT ISO-8859-1 mismatch: %q, err=%v", got, err)
	}

	var sltPayload bytes.Buffer
	sltPayload.WriteByte(3) // UTF-8
	sltPayload.WriteString("eng")
	sltPayload.WriteByte(2) // ms
	sltPayload.WriteByte(1)
	sltPayload.WriteByte(0) // empty descriptor
	sltPayload.WriteString("V22 Synced\x00")
	_ = binary.Write(&sltPayload, binary.BigEndian, uint32(12000))
	mp3v22SLT := buildID3v22MP3("v22_slt.mp3", "SLT", sltPayload.Bytes())
	if got, err := ExtractEmbeddedLyrics(mp3v22SLT); err != nil || got != "[00:12.000]V22 Synced" {
		t.Fatalf("ID3v2.2 SLT mismatch: %q, err=%v", got, err)
	}

	// 6. M4A / MP4 (moov -> udta -> meta -> ilst -> ©lyr -> data), including 64-bit box size and extension fallback
	var dataAtomPayload bytes.Buffer
	_ = binary.Write(&dataAtomPayload, binary.BigEndian, uint32(1)) // UTF-8 text indicator
	_ = binary.Write(&dataAtomPayload, binary.BigEndian, uint32(0)) // locale
	dataAtomPayload.WriteString("[00:15.25]M4A Atom Lyrics\x00")

	dataBox := buildMP4Box64("data", dataAtomPayload.Bytes())
	lyrBox := buildMP4Box("\xa9lyr", dataBox)
	ilstBox := buildMP4Box("ilst", lyrBox)
	metaPayload := append([]byte{0, 0, 0, 0}, ilstBox...) // 4B FullBox version/flags
	metaBox := buildMP4Box("meta", metaPayload)
	udtaBox := buildMP4Box("udta", metaBox)
	moovBox := buildMP4Box64("moov", udtaBox)
	ftypBox := buildMP4Box("ftyp", []byte("M4A \x00\x00\x00\x00"))

	m4aPath := filepath.Join(tmp, "song.m4a")
	_ = os.WriteFile(m4aPath, append(ftypBox, moovBox...), 0o644)
	if got, err := ExtractEmbeddedLyrics(m4aPath); err != nil || got != "[00:15.25]M4A Atom Lyrics" {
		t.Fatalf("M4A lyrics mismatch: %q, err=%v", got, err)
	}

	// Extension fallback when first box is an uncommon 4CC (e.g. "wide") followed by open-ended (size=0) moov
	wideBox := buildMP4Box("wide", []byte("12345678"))
	var openEndedMoov bytes.Buffer
	_ = binary.Write(&openEndedMoov, binary.BigEndian, uint32(0)) // boxSize == 0 -> until EOF
	openEndedMoov.WriteString("moov")
	openEndedMoov.Write(udtaBox)
	alacPath := filepath.Join(tmp, "fallback.alac")
	_ = os.WriteFile(alacPath, append(wideBox, openEndedMoov.Bytes()...), 0o644)
	if got, err := ExtractEmbeddedLyrics(alacPath); err != nil || got != "[00:15.25]M4A Atom Lyrics" {
		t.Fatalf("ALAC open-ended fallback mismatch: %q, err=%v", got, err)
	}

	// Open-ended (size=0) 'data' atom inside ©lyr + ID3v2 tagSize=0 & truncated tagSize>fileSize
	var openDataAtom bytes.Buffer
	_ = binary.Write(&openDataAtom, binary.BigEndian, uint32(0)) // size=0 until parent box end
	openDataAtom.WriteString("data")
	_ = binary.Write(&openDataAtom, binary.BigEndian, uint32(1))
	_ = binary.Write(&openDataAtom, binary.BigEndian, uint32(0))
	openDataAtom.WriteString("OpenDataLyrics")
	openM4A := filepath.Join(tmp, "opendata.m4a")
	_ = os.WriteFile(openM4A, append(ftypBox, buildMP4Box("moov", buildMP4Box("\xa9lyr", openDataAtom.Bytes()))...), 0o644)
	if got, err := ExtractEmbeddedLyrics(openM4A); err != nil || got != "OpenDataLyrics" {
		t.Fatalf("open-ended data atom mismatch: %q err=%v", got, err)
	}

	zeroID3 := filepath.Join(tmp, "zero_id3.mp3")
	_ = os.WriteFile(zeroID3, []byte{'I', 'D', '3', 3, 0, 0, 0, 0, 0, 0, 0, 0}, 0o644)
	if got, _ := ExtractEmbeddedLyrics(zeroID3); got != "" {
		t.Fatalf("expected empty lyrics on tagSize=0 ID3, got %q", got)
	}
	truncID3 := filepath.Join(tmp, "trunc_id3.mp3")
	_ = os.WriteFile(truncID3, []byte{'I', 'D', '3', 3, 0, 0, 0, 0, 1, 0, 'T', 'X'}, 0o644)
	if got, _ := ExtractEmbeddedLyrics(truncID3); got != "" {
		t.Fatalf("expected empty lyrics on truncated ID3, got %q", got)
	}
}

func TestV0110_AudioHLSPlaylistsSegmentsAndPolicies(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "audio_hls.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	libRepo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)
	cfg.RemoteClientBitrateLimit = 128_000
	cfg.LocalNetworkSubnets = []string{"203.0.113.0/28", "198.51.100.77"}
	cfg.FFmpegPath = createFakeFFmpegScript(t, "FAKE-TRANSCODED-AUDIO-DATA", 0)

	authSvc := auth.NewService(userRepo, cfg)
	mgr := transcode.NewManager(cfg, transcode.WithCapabilities(transcode.SoftwareOnlyCapabilities()))
	defer mgr.Close()

	admin, _ := authSvc.CreateUser(ctx, "admin", "adminpass", true)
	alice, _ := authSvc.CreateUser(ctx, "alice", "alicepass", false)
	noTrans, _ := authSvc.CreateUser(ctx, "notrans", "pass1234", false)
	restricted, _ := authSvc.CreateUser(ctx, "restricted", "pass1234", false)

	libMusic, _ := libRepo.CreateLibrary(ctx, "Music", "music", []string{tmp})
	libOther, _ := libRepo.CreateLibrary(ctx, "Other", "music", []string{tmp})

	noTransPol := sqlite.DefaultUserPolicy(false)
	noTransPol.EnableAudioPlaybackTranscoding = false
	_ = userRepo.UpdateUserPolicy(ctx, noTrans.ID, noTransPol)
	noTrans.Policy = noTransPol

	resPol := sqlite.DefaultUserPolicy(false)
	resPol.EnableAllFolders = false
	resPol.EnabledFolders = []string{libMusic.ID}
	resPol.BlockUnratedItems = []string{"Music"}
	resPol.AllowedTags = []string{"approved"}
	_ = userRepo.UpdateUserPolicy(ctx, restricted.ID, resPol)
	restricted.Policy = resPol

	flacFile := filepath.Join(tmp, "track.flac")
	// Write embedded FLAC lyrics so /Lyrics endpoint hits embedded fallback path!
	var vorbis bytes.Buffer
	_ = binary.Write(&vorbis, binary.LittleEndian, uint32(3))
	vorbis.WriteString("amb")
	_ = binary.Write(&vorbis, binary.LittleEndian, uint32(1))
	entry := "LYRICS=[by:Composer]\n[offset:-500]\n[00:00.2][00:02.050]Embedded Hook"
	_ = binary.Write(&vorbis, binary.LittleEndian, uint32(len(entry)))
	vorbis.WriteString(entry)
	var flacBytes bytes.Buffer
	flacBytes.WriteString("fLaC")
	flacBytes.Write([]byte{0x84, 0, 0, byte(vorbis.Len())})
	flacBytes.Write(vorbis.Bytes())
	_ = os.WriteFile(flacFile, flacBytes.Bytes(), 0o644)

	rt := int64(180 * ticksPerSecond)
	trackRated := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libMusic.ID, Type: "Audio", MediaType: "Audio",
		Name: "Rated Track", OfficialRating: "G", Container: "flac", Path: flacFile, RuntimeTicks: &rt,
	}
	trackUnrated := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libMusic.ID, Type: "Audio", MediaType: "Audio",
		Name: "Unrated Track", OfficialRating: "", Container: "flac", Path: flacFile,
	}
	trackNoTag := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libMusic.ID, Type: "Audio", MediaType: "Audio",
		Name: "NoTag Track", OfficialRating: "G", Container: "flac", Path: flacFile,
	}
	trackOtherLib := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libOther.ID, Type: "Audio", MediaType: "Audio",
		Name: "OtherLib Track", OfficialRating: "G", Container: "flac", Path: flacFile,
	}
	for _, it := range []*sqlite.ItemRecord{trackRated, trackUnrated, trackNoTag, trackOtherLib} {
		if err := libRepo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	_ = libRepo.ReplaceItemTaxonomies(ctx, trackRated.ID, "tag", []string{"approved"})
	_ = libRepo.ReplaceMediaSourcesAndStreams(ctx, trackRated.ID, []sqlite.MediaSourceRecord{
		{ID: trackRated.ID, ItemID: trackRated.ID, Path: flacFile, Container: "flac", Bitrate: 900_000},
	})

	h := New(libRepo, cfg)
	h.SetUserRepo(userRepo)
	h.SetTranscodeManager(mgr)

	var ctxFallbackUID string
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return ctxFallbackUID },
	})

	doReq := func(ac *middleware.AuthContext, method, target, remoteAddr string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		if remoteAddr != "" {
			req.RemoteAddr = remoteAddr
		}
		if ac != nil {
			req = req.WithContext(middleware.WithAuthContext(req.Context(), ac))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	adminAC := &middleware.AuthContext{User: admin, Token: "tok-admin"}
	aliceAC := &middleware.AuthContext{User: alice, Token: "tok-alice"}
	noTransAC := &middleware.AuthContext{User: noTrans, Token: "tok-nt"}
	resAC := &middleware.AuthContext{User: restricted, Token: "tok-res"}

	// 1. Extended policy & library RBAC checks (with UserRepo and without UserRepo)
	if code := doReq(resAC, http.MethodGet, "/Audio/"+trackUnrated.ID+"/stream", "127.0.0.1:1234").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 for unrated music, got %d", code)
	}
	if code := doReq(resAC, http.MethodGet, "/Audio/"+trackNoTag.ID+"/stream", "127.0.0.1:1234").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 for missing allowed tag, got %d", code)
	}
	if code := doReq(resAC, http.MethodGet, "/Audio/"+trackOtherLib.ID+"/stream", "127.0.0.1:1234").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 for disallowed library, got %d", code)
	}
	if code := doReq(resAC, http.MethodGet, "/Audio/"+trackRated.ID+"/stream", "127.0.0.1:1234").Code; code != http.StatusOK {
		t.Fatalf("expected 200 for rated+tagged music, got %d", code)
	}
	// Fallback UserIDFromCtx path in callerPolicy & canAccessItem
	ctxFallbackUID = admin.ID
	if code := doReq(nil, http.MethodGet, "/Audio/"+trackRated.ID+"/stream", "127.0.0.1:1234").Code; code != http.StatusOK {
		t.Fatalf("expected 200 via UserIDFromCtx admin, got %d", code)
	}
	ctxFallbackUID = alice.ID
	if code := doReq(nil, http.MethodGet, "/Audio/"+trackRated.ID+"/master.m3u8", "127.0.0.1:1234").Code; code != http.StatusOK {
		t.Fatalf("expected 200 via UserIDFromCtx alice, got %d", code)
	}
	ctxFallbackUID = ""
	if code := doReq(nil, http.MethodGet, "/Audio/"+trackRated.ID+"/stream", "127.0.0.1:1234").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 when unauthenticated with UserRepo set, got %d", code)
	}

	// Test hNoUserRepo branch in canAccessItem & callerPolicy
	hNoUR := New(libRepo, cfg)
	muxNoUR := http.NewServeMux()
	hNoUR.RegisterRoutes(muxNoUR, AuthFuncs{})
	for _, id := range []string{trackOtherLib.ID, trackUnrated.ID} {
		r := httptest.NewRequest(http.MethodGet, "/Audio/"+id+"/stream", nil)
		r = r.WithContext(middleware.WithAuthContext(r.Context(), resAC))
		w := httptest.NewRecorder()
		muxNoUR.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("no-userRepo canAccessItem expected 403 for %s, got %d", id, w.Code)
		}
	}
	rOK := httptest.NewRequest(http.MethodGet, "/Audio/"+trackRated.ID+"/stream", nil)
	rOK = rOK.WithContext(middleware.WithAuthContext(rOK.Context(), resAC))
	wOK := httptest.NewRecorder()
	muxNoUR.ServeHTTP(wOK, rOK)
	if wOK.Code != http.StatusOK {
		t.Fatalf("no-userRepo canAccessItem expected 200 for trackRated, got %d", wOK.Code)
	}

	// 2. Embedded lyrics via GET /Audio/{Id}/lyrics (fileSeg == "lyrics") + oversized external LRC 413
	recLyr := doReq(aliceAC, http.MethodGet, "/Audio/"+trackRated.ID+"/lyrics", "127.0.0.1:1234")
	if recLyr.Code != http.StatusOK {
		t.Fatalf("GET /Audio/{Id}/lyrics failed: %d %s", recLyr.Code, recLyr.Body.String())
	}
	var lyrDTO LyricDto
	_ = json.Unmarshal(recLyr.Body.Bytes(), &lyrDTO)
	if !lyrDTO.Metadata.IsSynced || lyrDTO.Metadata.Author != "Composer" || len(lyrDTO.Lyrics) != 2 || *lyrDTO.Lyrics[0].Start != 0 {
		t.Fatalf("unexpected embedded LyricDto: %+v", lyrDTO)
	}

	// Unsynced ParseLRC check
	unsyncedDTO := ParseLRC("Line A\nLine B\n")
	if unsyncedDTO.Metadata.IsSynced || len(unsyncedDTO.Lyrics) != 2 {
		t.Fatalf("expected 2 unsynced lines, got %+v", unsyncedDTO)
	}

	// Oversized external LRC (> 2 MiB) -> 413, and unreadable external LRC -> 403
	bigLRC := filepath.Join(tmp, "track.lrc")
	_ = os.WriteFile(bigLRC, bytes.Repeat([]byte("a"), maxExternalLRCBytes+10), 0o644)
	if code := doReq(aliceAC, http.MethodGet, "/Audio/"+trackRated.ID+"/Lyrics", "127.0.0.1:1234").Code; code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized .lrc, got %d", code)
	}
	if os.Geteuid() != 0 {
		_ = os.WriteFile(bigLRC, []byte("[00:01.00]NoPerm"), 0o644)
		_ = os.Chmod(bigLRC, 0o000)
		if code := doReq(aliceAC, http.MethodGet, "/Audio/"+trackRated.ID+"/Lyrics", "127.0.0.1:1234").Code; code != http.StatusForbidden {
			_ = os.Chmod(bigLRC, 0o644)
			t.Fatalf("expected 403 for unreadable .lrc, got %d", code)
		}
		_ = os.Chmod(bigLRC, 0o644)
	}
	_ = os.Remove(bigLRC)

	// 3. Audio HLS Playlists: master.m3u8, main.m3u8, live.m3u8, HEAD, remote bitrate clamp, transcode disabled 403
	if code := doReq(noTransAC, http.MethodGet, "/Audio/"+trackRated.ID+"/master.m3u8", "127.0.0.1:1234").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 on HLS playlist when audio transcode disabled, got %d", code)
	}
	if code := doReq(aliceAC, http.MethodGet, "/Audio/"+guid.New()+"/master.m3u8", "127.0.0.1:1234").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 on HLS playlist for missing item, got %d", code)
	}

	// Remote public IP (8.8.8.8) clamps BANDWIDTH to RemoteClientBitrateLimit (128000)
	recMaster := doReq(aliceAC, http.MethodGet, "/Audio/"+trackRated.ID+"/master.m3u8?AudioBitrate=320000&PlaySessionId=ps1&SegmentContainer=m4s", "8.8.8.8:54321")
	if recMaster.Code != http.StatusOK || !strings.Contains(recMaster.Body.String(), "BANDWIDTH=128000") || !strings.Contains(recMaster.Body.String(), "main.m3u8?") {
		t.Fatalf("unexpected master.m3u8 response: %d %s", recMaster.Code, recMaster.Body.String())
	}
	// Custom LocalNetworkSubnets (203.0.113.5 in 203.0.113.0/28 and 198.51.100.77 exact) do NOT clamp
	recSubnet := doReq(aliceAC, http.MethodGet, "/Audio/"+trackRated.ID+"/master.m3u8?AudioBitrate=320000", "203.0.113.5:54321")
	if recSubnet.Code != http.StatusOK || !strings.Contains(recSubnet.Body.String(), "BANDWIDTH=320000") {
		t.Fatalf("expected subnet IP not to clamp bitrate, got: %s", recSubnet.Body.String())
	}
	recSingleIP := doReq(aliceAC, http.MethodHead, "/Audio/"+trackRated.ID+"/main.m3u8?SegmentContainer=m4s", "198.51.100.77:54321")
	if recSingleIP.Code != http.StatusOK || recSingleIP.Body.Len() != 0 {
		t.Fatalf("expected 200 empty body on HEAD main.m3u8, got %d len=%d", recSingleIP.Code, recSingleIP.Body.Len())
	}
	recLive := doReq(adminAC, http.MethodGet, "/Audio/"+trackUnrated.ID+"/live.m3u8", "127.0.0.1:1234")
	if recLive.Code != http.StatusOK || !strings.Contains(recLive.Body.String(), "#EXTM3U") {
		t.Fatalf("expected 200 on live.m3u8 with default 240s duration, got %d", recLive.Code)
	}

	// 4. Audio HLS Segments (/Audio/{Id}/hls1/{PlaylistId}/{Segment} and /Audio/{Id}/hls/...)
	if code := doReq(noTransAC, http.MethodGet, "/Audio/"+trackRated.ID+"/hls1/main/seg_0.ts", "127.0.0.1:1234").Code; code != http.StatusForbidden {
		t.Fatalf("expected 403 on HLS segment when audio transcode disabled, got %d", code)
	}
	if code := doReq(adminAC, http.MethodGet, "/Audio/"+trackRated.ID+"/hls1/main/noext", "127.0.0.1:1234").Code; code != http.StatusBadRequest {
		t.Fatalf("expected 400 for segment without ext, got %d", code)
	}
	if code := doReq(adminAC, http.MethodGet, "/Audio/"+trackRated.ID+"/hls1/main/seg_bad.ts", "127.0.0.1:1234").Code; code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-numeric segment index, got %d", code)
	}
	if code := doReq(adminAC, http.MethodGet, "/Audio/"+guid.New()+"/hls1/main/seg_0.ts", "127.0.0.1:1234").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing audio item on segment, got %d", code)
	}

	// Pre-populate cached segments in transcode.Manager workDir for both ts and fmp4 (init.mp4 + seg_0.m4s)
	tsParams := transcode.SessionParams{
		PlaySessionID:    "ps-ts",
		UserID:           guid.Normalize(admin.ID),
		ItemID:           trackRated.ID,
		MediaSourceID:    trackRated.ID,
		InputPath:        flacFile,
		VideoCodec:       "none",
		AudioCodec:       "aac",
		AudioBitrate:     192_000,
		AudioChannels:    2,
		SegmentDuration:  transcode.DefaultSegmentDuration,
		SegmentContainer: "ts",
		DurationSec:      180,
		AudioOnly:        true,
	}
	snapTS, err := mgr.RegisterSession(tsParams)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(snapTS.WorkDir, "seg_0.ts"), []byte("TS-AUDIO-SEG"), 0o644)

	recSegTS := doReq(adminAC, http.MethodGet, "/Audio/"+trackRated.ID+"/hls1/ps-ts/seg_0.ts", "127.0.0.1:1234")
	if recSegTS.Code != http.StatusOK || recSegTS.Header().Get("Content-Type") != "audio/mp2t" || recSegTS.Body.String() != "TS-AUDIO-SEG" {
		t.Fatalf("HLS TS segment failed: %d ct=%s body=%q", recSegTS.Code, recSegTS.Header().Get("Content-Type"), recSegTS.Body.String())
	}

	fmp4Params := tsParams
	fmp4Params.PlaySessionID = "ps-m4s"
	fmp4Params.SegmentContainer = "m4s"
	snapM4S, err := mgr.RegisterSession(fmp4Params)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(snapM4S.WorkDir, "init.mp4"), []byte("FMP4-INIT-ATOM"), 0o644)
	_ = os.WriteFile(filepath.Join(snapM4S.WorkDir, "seg_0.m4s"), []byte("FMP4-AUDIO-SEG"), 0o644)

	recInit := doReq(adminAC, http.MethodGet, "/Audio/"+trackRated.ID+"/hls1/ps-m4s/init.mp4?SegmentContainer=m4s", "127.0.0.1:1234")
	if recInit.Code != http.StatusOK || recInit.Header().Get("Content-Type") != "audio/mp4" || recInit.Body.String() != "FMP4-INIT-ATOM" {
		t.Fatalf("HLS init.mp4 failed: %d ct=%s body=%q", recInit.Code, recInit.Header().Get("Content-Type"), recInit.Body.String())
	}
	recM4S := doReq(adminAC, http.MethodGet, "/Audio/"+trackRated.ID+"/hls/ps-m4s/segment0.m4s?SegmentContainer=m4s", "127.0.0.1:1234")
	if recM4S.Code != http.StatusOK || recM4S.Header().Get("Content-Type") != "audio/mp4" || recM4S.Body.String() != "FMP4-AUDIO-SEG" {
		t.Fatalf("HLS m4s segment failed: %d ct=%s body=%q", recM4S.Code, recM4S.Header().Get("Content-Type"), recM4S.Body.String())
	}

	// Nil transcodeMgr returns 404 on segment
	rNoMgr := httptest.NewRequest(http.MethodGet, "/Audio/"+trackRated.ID+"/hls1/main/0.ts", nil)
	wNoMgr := httptest.NewRecorder()
	muxNoUR.ServeHTTP(wNoMgr, rNoMgr)
	if wNoMgr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when transcodeMgr is nil, got %d", wNoMgr.Code)
	}

	// 5. Universal & pipe transcode branches, STRM with headers, proxyRemoteAudio, and helper coverage
	if code := doReq(adminAC, http.MethodGet, "/Audio/"+trackRated.ID+"/unsupported_endpoint", "127.0.0.1:1234").Code; code != http.StatusNotFound {
		t.Fatalf("expected 404 for unsupported audio endpoint, got %d", code)
	}
	// HEAD on universal transcode returns 200 without launching ffmpeg
	recHeadUni := doReq(aliceAC, http.MethodHead, "/Audio/"+trackRated.ID+"/universal.ogg?Container=ogg&AudioCodec=vorbis&StartTimeTicks=10000000", "127.0.0.1:1234")
	if recHeadUni.Code != http.StatusOK || recHeadUni.Header().Get("Content-Type") != "audio/ogg" {
		t.Fatalf("HEAD universal transcode failed: %d ct=%s", recHeadUni.Code, recHeadUni.Header().Get("Content-Type"))
	}
	// Live fake-ffmpeg pipe transcode on GET universal.mp4
	recPipe := doReq(aliceAC, http.MethodGet, "/Audio/"+trackRated.ID+"/universal.mp4?Container=mp4&AudioBitrate=9999999&AudioChannels=16&AudioSampleRate=384000&StartTimeTicks=20000000", "127.0.0.1:1234")
	if recPipe.Code != http.StatusOK || recPipe.Header().Get("Content-Type") != "audio/mp4" || recPipe.Body.String() != "FAKE-TRANSCODED-AUDIO-DATA" {
		t.Fatalf("GET universal transcode failed: %d ct=%s body=%q", recPipe.Code, recPipe.Header().Get("Content-Type"), recPipe.Body.String())
	}
	// Concurrency semaphore exhaustion -> 503
	for i := 0; i < maxConcurrentAudioTranscodes; i++ {
		h.transcodeSem <- struct{}{}
	}
	if code := doReq(aliceAC, http.MethodGet, "/Audio/"+trackRated.ID+"/universal.mp3?Container=mp3", "127.0.0.1:1234").Code; code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when transcodeSem full, got %d", code)
	}
	for i := 0; i < maxConcurrentAudioTranscodes; i++ {
		<-h.transcodeSem
	}

	// STRM resolution with custom headers + proxyRemoteAudioWithHeaders
	strmPath := filepath.Join(tmp, "remote_song.strm")
	_ = os.WriteFile(strmPath, []byte("https://93.184.216.34/music/song.opus\n"), 0o644)
	strmItem := &sqlite.ItemRecord{
		ID: guid.New(), LibraryID: libMusic.ID, Type: "Audio", MediaType: "Audio",
		Name: "STRM Opus", OfficialRating: "G", Container: "opus", Path: strmPath,
	}
	_ = libRepo.UpsertItem(ctx, strmItem)

	var capturedUA, capturedRange, capturedAuth string
	h.SetProxyClient(&http.Client{
		Transport: audioRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			capturedUA = r.Header.Get("User-Agent")
			capturedRange = r.Header.Get("Range")
			capturedAuth = r.Header.Get("Authorization")
			hdr := make(http.Header)
			hdr.Set("Content-Type", "text/html; charset=utf-8") // unsafe MIME -> sanitized to fallback/octet-stream
			hdr.Set("Accept-Ranges", "bytes")
			hdr.Set("Content-Length", "9")
			return &http.Response{
				StatusCode: http.StatusPartialContent,
				Header:     hdr,
				Body:       io.NopCloser(strings.NewReader("OPUS-DATA")),
			}, nil
		}),
	})
	h.SetStreamResolverWithHeaders(func(ctx context.Context, rawPath string) (*plugins.StreamResolveResult, error) {
		return &plugins.StreamResolveResult{
			URL: "https://93.184.216.34/music/song.opus",
			Headers: map[string]string{
				"Authorization": "Bearer secret-tok",
				"Bad\r\nHeader": "evil",
				"X-Ignored":     "ignored",
			},
			Is302: true, // Even with Is302=true, non-empty Headers forces server-side proxy!
		}, nil
	})

	reqProxy := httptest.NewRequest(http.MethodGet, "/Audio/"+strmItem.ID+"/stream", nil)
	reqProxy.Header.Set("Range", "bytes=0-8")
	reqProxy.Header.Set("If-Range", `"etag1"`)
	reqProxy = reqProxy.WithContext(middleware.WithAuthContext(reqProxy.Context(), adminAC))
	recProxy := httptest.NewRecorder()
	mux.ServeHTTP(recProxy, reqProxy)
	if recProxy.Code != http.StatusPartialContent || recProxy.Body.String() != "OPUS-DATA" {
		t.Fatalf("STRM proxy with headers failed: %d %q", recProxy.Code, recProxy.Body.String())
	}
	if capturedAuth != "Bearer secret-tok" || capturedRange != "bytes=0-8" || capturedUA == "" {
		t.Fatalf("proxy headers not forwarded: auth=%q range=%q ua=%q", capturedAuth, capturedRange, capturedUA)
	}
	if ct := recProxy.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("expected sanitized application/octet-stream, got %q", ct)
	}

	// Exercise proxyRemoteAudio wrapper, resolveStrmAudio/Full wrappers, resolveAudioTranscodeTarget & audioContentType tables
	wWrap := httptest.NewRecorder()
	rWrap := httptest.NewRequest(http.MethodHead, "/Audio/"+strmItem.ID+"/stream", nil)
	h.proxyRemoteAudio(wWrap, rWrap, "https://93.184.216.34/music/song.opus")
	if wWrap.Code != http.StatusPartialContent {
		t.Fatalf("proxyRemoteAudio HEAD expected 206, got %d", wWrap.Code)
	}
	h.SetStreamResolverWithHeaders(nil)
	h.SetStreamResolverFull(func(ctx context.Context, rawPath string) (string, bool, error) {
		return "https://93.184.216.34/music/a.mp3", true, nil
	})
	if u, err := h.resolveStrmAudio(ctx, strmPath); err != nil || u != "https://93.184.216.34/music/a.mp3" {
		t.Fatalf("resolveStrmAudio failed: %q %v", u, err)
	}
	h.SetStreamResolverFull(nil)
	h.SetStreamResolver(func(ctx context.Context, rawPath string) (string, error) {
		return "", errors.New("resolver boom")
	})
	if _, err := h.resolveStrmAudio(ctx, strmPath); err == nil {
		t.Fatalf("expected error from failing streamResolver")
	}

	for _, tc := range []struct{ c, codec, wantFmt string }{
		{"aac", "", "adts"},
		{"m4a", "", "mp4"},
		{"ogg", "vorbis", "ogg"},
		{"opus", "opus", "ogg"},
		{"flac", "", "flac"},
		{"wav", "", "wav"},
		{"", "aac", "adts"},
		{"mp3", "", "mp3"},
	} {
		if _, fmtGot, _ := resolveAudioTranscodeTarget(tc.c, tc.codec); fmtGot != tc.wantFmt {
			t.Fatalf("resolveAudioTranscodeTarget(%q,%q) = %q, want %q", tc.c, tc.codec, fmtGot, tc.wantFmt)
		}
	}
	for _, ext := range []string{".mp3", ".flac", ".m4a", ".aac", ".ogg", ".opus", ".wav", ".wma", ".xyz"} {
		if audioContentType("track"+ext) == "" {
			t.Fatalf("empty content type for %s", ext)
		}
	}
	for _, mt := range []string{"", "audio/flac", "application/vnd.apple.mpegurl", "invalid;;mime"} {
		_ = sanitizeProxyAudioContentType(mt, "audio/mpeg")
	}

	// Stream limit 429 on HLS playlist, HLS segment, and pipe transcode (mgr already has active sessions >= 1)
	cfg.SimultaneousStreamLimit = 1
	if code := doReq(adminAC, http.MethodGet, "/Audio/"+trackRated.ID+"/master.m3u8?PlaySessionId=new-ps", "127.0.0.1:1234").Code; code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on HLS playlist when stream limit reached, got %d", code)
	}
	if code := doReq(adminAC, http.MethodGet, "/Audio/"+trackRated.ID+"/hls1/new-ps/seg_0.ts", "127.0.0.1:1234").Code; code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on HLS segment when stream limit reached, got %d", code)
	}
	if code := doReq(adminAC, http.MethodGet, "/Audio/"+trackRated.ID+"/universal.mp3?Container=mp3", "127.0.0.1:1234").Code; code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on pipe transcode when stream limit reached, got %d", code)
	}
	cfg.SimultaneousStreamLimit = 0

	// HTTP input to streamTranscodeAudioWithHeaders (starts loopback proxy + runs fake ffmpeg)
	wHTTPTrans := httptest.NewRecorder()
	h.streamTranscodeAudioWithHeaders(wHTTPTrans, httptest.NewRequest(http.MethodGet, "/", nil), "http://192.168.1.250:65530/song.flac", map[string]string{"Authorization": "Bearer x"}, "flac", "flac", 300000, 0, 2, 44100)
	if wHTTPTrans.Code != http.StatusOK {
		t.Fatalf("HTTP streamTranscodeAudioWithHeaders expected 200, got %d %s", wHTTPTrans.Code, wHTTPTrans.Body.String())
	}

	// STRM failure on HLS playlist & HLS segment -> 502 BadGateway
	h.SetStreamResolver(func(ctx context.Context, rawPath string) (string, error) {
		return "", errors.New("strm broken")
	})
	if code := doReq(adminAC, http.MethodGet, "/Audio/"+strmItem.ID+"/master.m3u8", "127.0.0.1:1234").Code; code != http.StatusBadGateway {
		t.Fatalf("expected 502 on HLS playlist when STRM fails, got %d", code)
	}
	if code := doReq(adminAC, http.MethodGet, "/Audio/"+strmItem.ID+"/hls1/main/seg_0.ts", "127.0.0.1:1234").Code; code != http.StatusBadGateway {
		t.Fatalf("expected 502 on HLS segment when STRM fails, got %d", code)
	}
	if firstNonEmptyAudio("", "  ") != "" || !matchesBlockUnratedAudio("Audio", "", []string{"", "*"}) {
		t.Fatalf("unexpected helper return")
	}
	if h.CanAccessLibrary(ctx, "", libMusic.ID) || h.CanAccessLibrary(ctx, "missing-user", libMusic.ID) {
		t.Fatalf("expected CanAccessLibrary=false for empty or missing user")
	}

	// Without transcodeMgr, activePipeStreams enforces SimultaneousStreamLimit
	cfg.SimultaneousStreamLimit = 1
	hNoUR.activePipeStreams.Store(1)
	wLim := httptest.NewRecorder()
	rLim := httptest.NewRequest(http.MethodGet, "/Audio/"+trackRated.ID+"/universal.mp3", nil)
	hNoUR.streamTranscodeAudio(wLim, rLim, flacFile, "mp3", "mp3", 128000, 0, 2, 44100)
	if wLim.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 via activePipeStreams limit, got %d", wLim.Code)
	}
	hNoUR.activePipeStreams.Store(0)
	cfg.SimultaneousStreamLimit = 0

	// Missing file on disk in serveAudioFile and streamTranscodeAudioWithHeaders -> 404
	wMiss1 := httptest.NewRecorder()
	serveAudioFile(wMiss1, httptest.NewRequest(http.MethodGet, "/", nil), filepath.Join(tmp, "gone.mp3"))
	if wMiss1.Code != http.StatusNotFound {
		t.Fatalf("expected 404 in serveAudioFile for missing file, got %d", wMiss1.Code)
	}
	wDir1 := httptest.NewRecorder()
	serveAudioFile(wDir1, httptest.NewRequest(http.MethodGet, "/", nil), tmp)
	if wDir1.Code != http.StatusNotFound {
		t.Fatalf("expected 404 in serveAudioFile for dir, got %d", wDir1.Code)
	}
	wMiss2 := httptest.NewRecorder()
	hNoUR.streamTranscodeAudio(wMiss2, httptest.NewRequest(http.MethodGet, "/", nil), filepath.Join(tmp, "gone.mp3"), "mp3", "", 128000, 0, 0, 0)
	if wMiss2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 in streamTranscodeAudio for missing file, got %d", wMiss2.Code)
	}
	wBlocked := httptest.NewRecorder()
	hNoUR.streamTranscodeAudio(wBlocked, httptest.NewRequest(http.MethodGet, "/", nil), "http://169.254.169.254/audio.mp3", "mp3", "", 128000, 0, 0, 0)
	if wBlocked.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 in streamTranscodeAudio for link-local SSRF URL, got %d", wBlocked.Code)
	}
}
