package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/api/middleware"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/pkg/netutil"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestV0109Audit_FFmpegHardeningAndExtensionlessUniversal302(t *testing.T) {
	// 1. [C-02 & H-07] HTTP input enforces -protocol_whitelist, -max_redirects 0, -rw_timeout 15000000
	httpArgsSlice := buildAudioFFmpegArgs("https://93.184.216.34/audio/stream", "mp3", "libmp3lame", 192000, 0, 2, 44100)
	httpArgs := strings.Join(httpArgsSlice, " ")
	for _, want := range []string{
		"-protocol_whitelist http,https,tcp,tls,crypto",
		"-max_redirects 0",
		"-rw_timeout 15000000",
	} {
		if !strings.Contains(httpArgs, want) {
			t.Fatalf("expected %q in HTTP ffmpeg args, got: %s", want, httpArgs)
		}
	}
	if strings.Contains(httpArgs, "-follow_redirects") {
		t.Fatalf("must not contain invalid -follow_redirects flag: %s", httpArgs)
	}
	if strings.Index(httpArgs, "-max_redirects 0") > strings.Index(httpArgs, "-i ") {
		t.Fatalf("expected -max_redirects before -i, got: %s", httpArgs)
	}

	// Real ffmpeg smoke test when ffmpeg is available on host PATH
	if ffmpegBin, lookErr := exec.LookPath("ffmpeg"); lookErr == nil {
		lnSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://127.0.0.1:9/blocked", http.StatusFound)
		}))
		defer lnSrv.Close()
		smokeArgs := netutil.FilterFFmpegArgs(ffmpegBin, buildAudioFFmpegArgs(lnSrv.URL+"/audio.mp3", "mp3", "libmp3lame", 128000, 0, 2, 44100))
		smokeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, _ := exec.CommandContext(smokeCtx, ffmpegBin, smokeArgs...).CombinedOutput()
		if strings.Contains(string(out), "Unrecognized option") || strings.Contains(string(out), "Option not found") {
			t.Fatalf("real ffmpeg rejected audio args (%v): %s", smokeArgs, string(out))
		}
	}

	// Local file input enforces -protocol_whitelist file,crypto,data
	localArgs := strings.Join(buildAudioFFmpegArgs("/music/track.flac", "mp3", "libmp3lame", 192000, 50_000_000, 2, 44100), " ")
	if !strings.Contains(localArgs, "-protocol_whitelist file,crypto,data") {
		t.Fatalf("expected local -protocol_whitelist in ffmpeg args, got: %s", localArgs)
	}
	if strings.Index(localArgs, "-protocol_whitelist file,crypto,data") > strings.Index(localArgs, "-i ") {
		t.Fatalf("expected local -protocol_whitelist before -i, got: %s", localArgs)
	}

	// 2. [M-16] resolveAudioPath returns Container and handleUniversal handles extensionless signed STRM URLs
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "audio_v0109.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	cfg := config.Default(tmp)

	strmWithContainer := filepath.Join(tmp, "cloud_flac.strm")
	_ = os.WriteFile(strmWithContainer, []byte("https://93.184.216.34/download?token=abc123\n"), 0o644)
	itemWithContainer := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemWithContainer,
		LibraryID: "lib1",
		Type:      "Audio",
		MediaType: "Audio",
		Name:      "Cloud FLAC",
		Path:      strmWithContainer,
		Container: "flac",
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, itemWithContainer, []sqlite.MediaSourceRecord{
		{ID: guid.New(), ItemID: itemWithContainer, Path: strmWithContainer, Container: "flac", Bitrate: 800_000},
	})

	strmUnknownExt := filepath.Join(tmp, "cloud_unknown.strm")
	_ = os.WriteFile(strmUnknownExt, []byte("https://93.184.216.34/d/signed-object-id\n"), 0o644)
	itemUnknownExt := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{
		ID:        itemUnknownExt,
		LibraryID: "lib1",
		Type:      "Audio",
		MediaType: "Audio",
		Name:      "Cloud Unknown",
		Path:      strmUnknownExt,
	})

	h := New(repo, cfg)
	// Verify resolveAudioPath returns container
	_, _, gotContainer, err := h.resolveAudioPath(ctx, itemWithContainer, "")
	if err != nil || gotContainer != "flac" {
		t.Fatalf("resolveAudioPath expected container=flac, got %q err=%v", gotContainer, err)
	}

	signedURL := "https://93.184.216.34/d/signed-object-id?sig=xyz"
	h.SetStreamResolverFull(func(_ context.Context, _ string) (string, bool, error) {
		return signedURL, true, nil
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, AuthFuncs{})

	// 2a. Extensionless URL falls back to Container="flac" -> allowed when client accepts flac -> 302
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Audio/"+itemWithContainer+"/universal?Container=mp3,flac", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != signedURL {
		t.Fatalf("expected 302 redirect via Container fallback, got %d loc=%q", rec.Code, rec.Header().Get("Location"))
	}

	// 2b. Extensionless URL with empty Container + is302=true + no seek/bitrate limit -> 302 redirect instead of transcode
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/Audio/"+itemUnknownExt+"/universal?Container=mp3,aac", nil))
	if rec2.Code != http.StatusFound || rec2.Header().Get("Location") != signedURL {
		t.Fatalf("expected 302 redirect for extensionless signed STRM URL, got %d loc=%q", rec2.Code, rec2.Header().Get("Location"))
	}

	// 2c. When StartTimeTicks > 0, must NOT 302 redirect (falls through to transcode; HEAD returns 200 audio/mpeg)
	recSeek := httptest.NewRecorder()
	mux.ServeHTTP(recSeek, httptest.NewRequest(http.MethodHead, "/Audio/"+itemUnknownExt+"/universal?Container=mp3&StartTimeTicks=100000000", nil))
	if recSeek.Code == http.StatusFound {
		t.Fatalf("expected seek request not to 302 redirect, got 302")
	}

	// 2d. When bitrate exceeds MaxStreamingBitrate, must NOT 302 redirect
	recBitrate := httptest.NewRecorder()
	mux.ServeHTTP(recBitrate, httptest.NewRequest(http.MethodHead, "/Audio/"+itemWithContainer+"/universal?Container=mp3,flac&MaxStreamingBitrate=128000", nil))
	if recBitrate.Code == http.StatusFound {
		t.Fatalf("expected bitrate-exceeded request not to 302 redirect, got 302")
	}

	// 3. [H-05] EnableAudioPlaybackTranscoding=false returns 403 Forbidden
	noTCAudioUser := &sqlite.UserRecord{
		ID: guid.New(), Name: "notc",
		Policy: dto.UserPolicy{EnableAllFolders: true, EnableAudioPlaybackTranscoding: false},
	}
	reqNoTC := httptest.NewRequest(http.MethodGet, "/Audio/"+itemWithContainer+"/universal?Container=mp3&MaxStreamingBitrate=128000", nil)
	reqNoTC = reqNoTC.WithContext(middleware.WithAuthContext(reqNoTC.Context(), &middleware.AuthContext{User: noTCAudioUser}))
	recNoTC := httptest.NewRecorder()
	mux.ServeHTTP(recNoTC, reqNoTC)
	if recNoTC.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when EnableAudioPlaybackTranscoding=false, got %d", recNoTC.Code)
	}

	// 4. External .lrc > 2 MiB rejected + Pure-Go embedded lyrics (FLAC Vorbis, MP3 ID3v2 USLT/SYLT, M4A ©lyr)
	bigAudio := filepath.Join(tmp, "big.mp3")
	_ = os.WriteFile(bigAudio, []byte("mp3"), 0o644)
	_ = os.WriteFile(filepath.Join(tmp, "big.lrc"), bytes.Repeat([]byte("a"), maxExternalLRCBytes+10), 0o644)
	bigID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: bigID, LibraryID: "lib1", Type: "Audio", MediaType: "Audio", Name: "BigLRC", Path: bigAudio})
	recBig := httptest.NewRecorder()
	mux.ServeHTTP(recBig, httptest.NewRequest(http.MethodGet, "/Audio/"+bigID+"/Lyrics", nil))
	if recBig.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for >2MiB .lrc, got %d", recBig.Code)
	}

	// 4a. FLAC Vorbis Comment LYRICS
	flacFile := filepath.Join(tmp, "emb.flac")
	comment := "LYRICS=[00:02.00]FLAC Synced Line\n"
	var vorbis bytes.Buffer
	_ = binary.Write(&vorbis, binary.LittleEndian, uint32(3))
	vorbis.WriteString("ref")
	_ = binary.Write(&vorbis, binary.LittleEndian, uint32(1))
	_ = binary.Write(&vorbis, binary.LittleEndian, uint32(len(comment)))
	vorbis.WriteString(comment)
	var flacBuf bytes.Buffer
	flacBuf.WriteString("fLaC")
	vLen := vorbis.Len()
	flacBuf.Write([]byte{0x80 | 4, byte(vLen >> 16), byte(vLen >> 8), byte(vLen)})
	flacBuf.Write(vorbis.Bytes())
	_ = os.WriteFile(flacFile, flacBuf.Bytes(), 0o644)
	flacID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: flacID, LibraryID: "lib1", Type: "Audio", MediaType: "Audio", Name: "FLACLyr", Path: flacFile})

	recFLAC := httptest.NewRecorder()
	mux.ServeHTTP(recFLAC, httptest.NewRequest(http.MethodGet, "/Audio/"+flacID+"/Lyrics", nil))
	if recFLAC.Code != http.StatusOK {
		t.Fatalf("FLAC embedded lyrics failed: %d %s", recFLAC.Code, recFLAC.Body.String())
	}
	var flacDTO LyricDto
	_ = json.Unmarshal(recFLAC.Body.Bytes(), &flacDTO)
	if !flacDTO.Metadata.IsSynced || len(flacDTO.Lyrics) != 1 || flacDTO.Lyrics[0].Text != "FLAC Synced Line" {
		t.Fatalf("unexpected FLAC lyrics DTO: %+v", flacDTO)
	}

	// 4b. MP3 ID3v2.3 USLT
	mp3File := filepath.Join(tmp, "emb.mp3")
	var usltPayload bytes.Buffer
	usltPayload.WriteByte(3) // UTF-8
	usltPayload.WriteString("eng")
	usltPayload.WriteByte(0) // empty descriptor
	usltPayload.WriteString("MP3 Embedded Unsynced Line")
	var id3Frame bytes.Buffer
	id3Frame.WriteString("USLT")
	_ = binary.Write(&id3Frame, binary.BigEndian, uint32(usltPayload.Len()))
	id3Frame.Write([]byte{0, 0})
	id3Frame.Write(usltPayload.Bytes())
	fSz := id3Frame.Len()
	var mp3Buf bytes.Buffer
	mp3Buf.WriteString("ID3")
	mp3Buf.Write([]byte{3, 0, 0, byte((fSz >> 21) & 0x7F), byte((fSz >> 14) & 0x7F), byte((fSz >> 7) & 0x7F), byte(fSz & 0x7F)})
	mp3Buf.Write(id3Frame.Bytes())
	_ = os.WriteFile(mp3File, mp3Buf.Bytes(), 0o644)
	mp3ID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: mp3ID, LibraryID: "lib1", Type: "Audio", MediaType: "Audio", Name: "MP3Lyr", Path: mp3File})

	recMP3 := httptest.NewRecorder()
	mux.ServeHTTP(recMP3, httptest.NewRequest(http.MethodGet, "/Audio/"+mp3ID+"/Lyrics", nil))
	if recMP3.Code != http.StatusOK || !strings.Contains(recMP3.Body.String(), "MP3 Embedded Unsynced Line") {
		t.Fatalf("MP3 USLT embedded lyrics failed: %d %s", recMP3.Code, recMP3.Body.String())
	}

	// 4c. M4A moov->udta->meta->ilst->©lyr->data
	makeBox := func(typ string, payload []byte) []byte {
		var b bytes.Buffer
		_ = binary.Write(&b, binary.BigEndian, uint32(8+len(payload)))
		b.WriteString(typ)
		b.Write(payload)
		return b.Bytes()
	}
	m4aText := "[00:05.00]M4A Synced Lyric"
	dataPayload := append([]byte{0, 0, 0, 1, 0, 0, 0, 0}, []byte(m4aText)...)
	lyrBox := makeBox("\xa9lyr", makeBox("data", dataPayload))
	ilstBox := makeBox("ilst", lyrBox)
	metaBox := makeBox("meta", append([]byte{0, 0, 0, 0}, ilstBox...))
	udtaBox := makeBox("udta", metaBox)
	moovBox := makeBox("moov", udtaBox)
	ftypBox := makeBox("ftyp", []byte("M4A \x00\x00\x00\x00"))
	m4aFile := filepath.Join(tmp, "emb.m4a")
	_ = os.WriteFile(m4aFile, append(ftypBox, moovBox...), 0o644)
	m4aID := guid.New()
	_ = repo.UpsertItem(ctx, &sqlite.ItemRecord{ID: m4aID, LibraryID: "lib1", Type: "Audio", MediaType: "Audio", Name: "M4ALyr", Path: m4aFile})

	recM4A := httptest.NewRecorder()
	mux.ServeHTTP(recM4A, httptest.NewRequest(http.MethodGet, "/Audio/"+m4aID+"/Lyrics", nil))
	if recM4A.Code != http.StatusOK || !strings.Contains(recM4A.Body.String(), "M4A Synced Lyric") {
		t.Fatalf("M4A embedded lyrics failed: %d %s", recM4A.Code, recM4A.Body.String())
	}
}
