package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

const (
	maxExternalLRCBytes   = 2 << 20 // 2 MiB limit for external .lrc files
	maxEmbeddedTagBlock   = 4 << 20 // 4 MiB safety cap per metadata block/atom
	maxMP4ScanDepth       = 8
)

// ExtractEmbeddedLyrics extracts embedded lyrics from local audio files in pure Go
// without invoking FFmpeg or reading entire large audio streams into memory.
// Supported formats:
//   - FLAC Vorbis Comment (LYRICS, UNSYNCEDLYRICS)
//   - MP3 ID3v2.2/v2.3/v2.4 (USLT, SYLT, TXXX:LYRICS)
//   - MP4/M4A ISO BMFF (moov -> udta -> meta -> ilst -> ©lyr -> data)
func ExtractEmbeddedLyrics(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil || st.IsDir() || st.Size() < 8 {
		return "", nil
	}

	var magic [12]byte
	n, _ := io.ReadFull(f, magic[:])
	if n < 4 {
		return "", nil
	}

	switch {
	case string(magic[:4]) == "fLaC":
		return extractFLACVorbisLyrics(f, st.Size())
	case string(magic[:3]) == "ID3":
		return extractID3v2Lyrics(f, st.Size())
	case n >= 8 && (string(magic[4:8]) == "ftyp" || string(magic[4:8]) == "moov" || string(magic[4:8]) == "mdat" || string(magic[4:8]) == "free"):
		return extractMP4Lyrics(f, 0, st.Size(), 0)
	}

	// Fallback by extension if magic had leading padding
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".m4a", ".mp4", ".alac":
		return extractMP4Lyrics(f, 0, st.Size(), 0)
	}
	return "", nil
}

func extractFLACVorbisLyrics(f *os.File, fileSize int64) (string, error) {
	offset := int64(4) // right after "fLaC"
	var hdr [4]byte
	var fallback string

	for offset+4 <= fileSize {
		if _, err := f.ReadAt(hdr[:], offset); err != nil {
			break
		}
		isLast := (hdr[0] & 0x80) != 0
		blockType := hdr[0] & 0x7F
		blockLen := int64(hdr[1])<<16 | int64(hdr[2])<<8 | int64(hdr[3])
		offset += 4
		if blockLen < 0 || offset+blockLen > fileSize {
			break
		}
		if blockType == 4 && blockLen > 0 && blockLen <= maxEmbeddedTagBlock {
			buf := make([]byte, blockLen)
			if _, err := f.ReadAt(buf, offset); err == nil {
				lyrics, unsynced := parseVorbisCommentLyrics(buf)
				if lyrics != "" {
					return lyrics, nil
				}
				if unsynced != "" && fallback == "" {
					fallback = unsynced
				}
			}
		}
		offset += blockLen
		if isLast {
			break
		}
	}
	return fallback, nil
}

func parseVorbisCommentLyrics(data []byte) (lyrics string, unsynced string) {
	if len(data) < 8 {
		return "", ""
	}
	vendorLen := int(binary.LittleEndian.Uint32(data[0:4]))
	pos := 4 + vendorLen
	if vendorLen < 0 || pos+4 > len(data) {
		return "", ""
	}
	count := int(binary.LittleEndian.Uint32(data[pos : pos+4]))
	pos += 4
	for i := 0; i < count && pos+4 <= len(data); i++ {
		cLen := int(binary.LittleEndian.Uint32(data[pos : pos+4]))
		pos += 4
		if cLen < 0 || pos+cLen > len(data) {
			break
		}
		entry := string(data[pos : pos+cLen])
		pos += cLen
		eq := strings.IndexByte(entry, '=')
		if eq <= 0 {
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(entry[:eq]))
		val := strings.TrimSpace(entry[eq+1:])
		if val == "" {
			continue
		}
		switch key {
		case "LYRICS", "SYNCEDLYRICS":
			if lyrics == "" {
				lyrics = val
			}
		case "UNSYNCEDLYRICS", "UNSYNCED LYRICS":
			if unsynced == "" {
				unsynced = val
			}
		}
	}
	return lyrics, unsynced
}

func syncsafeToInt(b []byte) int {
	if len(b) < 4 {
		return 0
	}
	return (int(b[0]&0x7F) << 21) | (int(b[1]&0x7F) << 14) | (int(b[2]&0x7F) << 7) | int(b[3]&0x7F)
}

func removeID3Unsynchronisation(b []byte) []byte {
	if !bytes.Contains(b, []byte{0xFF, 0x00}) {
		return b
	}
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		out = append(out, b[i])
		if b[i] == 0xFF && i+1 < len(b) && b[i+1] == 0x00 {
			i++
		}
	}
	return out
}

func extractID3v2Lyrics(f *os.File, fileSize int64) (string, error) {
	var hdr [10]byte
	if _, err := f.ReadAt(hdr[:], 0); err != nil {
		return "", nil
	}
	if string(hdr[0:3]) != "ID3" {
		return "", nil
	}
	majorVer := hdr[3]
	flags := hdr[5]
	tagSize := syncsafeToInt(hdr[6:10])
	if tagSize <= 0 {
		return "", nil
	}
	if int64(10+tagSize) > fileSize {
		tagSize = int(fileSize - 10)
	}
	if tagSize <= 0 {
		return "", nil
	}
	readCap := tagSize
	if readCap > maxEmbeddedTagBlock {
		readCap = maxEmbeddedTagBlock
	}
	buf := make([]byte, readCap)
	if _, err := f.ReadAt(buf, 10); err != nil && err != io.EOF {
		return "", nil
	}
	if (flags & 0x80) != 0 {
		buf = removeID3Unsynchronisation(buf)
	}

	pos := 0
	// Skip ID3v2.3 / ID3v2.4 extended header if present
	if (flags & 0x40) != 0 {
		if len(buf) < 4 {
			return "", nil
		}
		var extSize int
		if majorVer == 4 {
			extSize = syncsafeToInt(buf[0:4])
			if extSize >= 4 {
				pos += extSize
			}
		} else {
			extSize = int(binary.BigEndian.Uint32(buf[0:4]))
			pos += 4 + extSize
		}
	}

	var unsyncedFallback string
	for pos < len(buf) {
		if majorVer == 2 {
			if pos+6 > len(buf) || buf[pos] == 0 {
				break
			}
			frameID := string(buf[pos : pos+3])
			frameSize := int(buf[pos+3])<<16 | int(buf[pos+4])<<8 | int(buf[pos+5])
			pos += 6
			if frameSize <= 0 || pos+frameSize > len(buf) {
				break
			}
			payload := buf[pos : pos+frameSize]
			pos += frameSize
			switch frameID {
			case "SLT":
				if s := parseID3SYLT(payload); s != "" {
					return s, nil
				}
			case "ULT":
				if s := parseID3USLT(payload); s != "" && unsyncedFallback == "" {
					unsyncedFallback = s
				}
			}
			continue
		}

		if pos+10 > len(buf) || buf[pos] == 0 {
			break
		}
		frameID := string(buf[pos : pos+4])
		var frameSize int
		if majorVer == 4 {
			frameSize = syncsafeToInt(buf[pos+4 : pos+8])
		} else {
			frameSize = int(binary.BigEndian.Uint32(buf[pos+4 : pos+8]))
		}
		frameFlags2 := buf[pos+9]
		pos += 10
		if frameSize <= 0 || pos+frameSize > len(buf) {
			break
		}
		payload := buf[pos : pos+frameSize]
		pos += frameSize

		// Handle frame-level data length indicator & unsynchronisation in v2.4
		if majorVer == 4 {
			if (frameFlags2 & 0x01) != 0 {
				if len(payload) < 4 {
					continue
				}
				payload = payload[4:]
			}
			if (frameFlags2 & 0x02) != 0 {
				payload = removeID3Unsynchronisation(payload)
			}
		}

		switch frameID {
		case "SYLT":
			if s := parseID3SYLT(payload); s != "" {
				return s, nil
			}
		case "USLT":
			if s := parseID3USLT(payload); s != "" {
				if strings.Contains(s, "[") && strings.Contains(s, "]") {
					return s, nil
				}
				if unsyncedFallback == "" {
					unsyncedFallback = s
				}
			}
		case "TXXX":
			if k, v := parseID3TXXX(payload); v != "" {
				ku := strings.ToUpper(strings.TrimSpace(k))
				if (ku == "LYRICS" || ku == "UNSYNCEDLYRICS") && unsyncedFallback == "" {
					unsyncedFallback = v
				}
			}
		}
	}
	return unsyncedFallback, nil
}

func splitID3NullTerminated(data []byte, encoding byte) (head []byte, rest []byte) {
	switch encoding {
	case 1, 2: // UTF-16 / UTF-16BE uses 2-byte aligned 0x00 0x00 terminator
		for i := 0; i+1 < len(data); i += 2 {
			if data[i] == 0 && data[i+1] == 0 {
				return data[:i], data[i+2:]
			}
		}
		return data, nil
	default: // ISO-8859-1 (0) or UTF-8 (3)
		idx := bytes.IndexByte(data, 0)
		if idx < 0 {
			return data, nil
		}
		return data[:idx], data[idx+1:]
	}
}

func decodeID3Text(b []byte, encoding byte) string {
	if len(b) == 0 {
		return ""
	}
	switch encoding {
	case 0: // ISO-8859-1
		runes := make([]rune, len(b))
		for i, c := range b {
			runes[i] = rune(c)
		}
		return strings.TrimRight(string(runes), "\x00")
	case 1: // UTF-16 with BOM
		bo := binary.ByteOrder(binary.LittleEndian)
		if len(b) >= 2 {
			if b[0] == 0xFE && b[1] == 0xFF {
				bo = binary.BigEndian
				b = b[2:]
			} else if b[0] == 0xFF && b[1] == 0xFE {
				bo = binary.LittleEndian
				b = b[2:]
			}
		}
		u16 := make([]uint16, len(b)/2)
		for i := range u16 {
			u16[i] = bo.Uint16(b[i*2 : i*2+2])
		}
		return strings.TrimRight(string(utf16.Decode(u16)), "\x00")
	case 2: // UTF-16BE without BOM
		u16 := make([]uint16, len(b)/2)
		for i := range u16 {
			u16[i] = binary.BigEndian.Uint16(b[i*2 : i*2+2])
		}
		return strings.TrimRight(string(utf16.Decode(u16)), "\x00")
	default: // 3 = UTF-8
		return strings.TrimRight(string(b), "\x00")
	}
}

func parseID3USLT(payload []byte) string {
	// 1B encoding + 3B language + null-terminated descriptor + text
	if len(payload) < 5 {
		return ""
	}
	enc := payload[0]
	_, body := splitID3NullTerminated(payload[4:], enc)
	return strings.TrimSpace(decodeID3Text(body, enc))
}

func parseID3TXXX(payload []byte) (string, string) {
	if len(payload) < 2 {
		return "", ""
	}
	enc := payload[0]
	desc, val := splitID3NullTerminated(payload[1:], enc)
	return decodeID3Text(desc, enc), strings.TrimSpace(decodeID3Text(val, enc))
}

func parseID3SYLT(payload []byte) string {
	// 1B encoding + 3B language + 1B timestamp format (2 = ms) + 1B content type + descriptor + sync items
	if len(payload) < 7 {
		return ""
	}
	enc := payload[0]
	tsFormat := payload[4]
	if tsFormat != 2 {
		// MPEG frames format cannot be converted accurately without VBR frame table; skip if non-ms
		return ""
	}
	_, rest := splitID3NullTerminated(payload[6:], enc)
	var sb strings.Builder
	for len(rest) > 0 {
		textBytes, afterText := splitID3NullTerminated(rest, enc)
		if len(afterText) < 4 {
			break
		}
		ms := binary.BigEndian.Uint32(afterText[:4])
		rest = afterText[4:]
		line := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(decodeID3Text(textBytes, enc), "\r", ""), "\n", " "))
		mins := ms / 60000
		secs := (ms % 60000) / 1000
		millis := ms % 1000
		fmt.Fprintf(&sb, "[%02d:%02d.%03d]%s\n", mins, secs, millis, line)
	}
	return strings.TrimSpace(sb.String())
}

func extractMP4Lyrics(f *os.File, start, end int64, depth int) (string, error) {
	if depth > maxMP4ScanDepth || start >= end {
		return "", nil
	}
	var hdr [16]byte
	offset := start
	for offset+8 <= end {
		if _, err := f.ReadAt(hdr[:8], offset); err != nil {
			break
		}
		boxSize := int64(binary.BigEndian.Uint32(hdr[0:4]))
		boxType := string(hdr[4:8])
		hdrLen := int64(8)
		if boxSize == 1 {
			if offset+16 > end {
				break
			}
			if _, err := f.ReadAt(hdr[8:16], offset+8); err != nil {
				break
			}
			boxSize = int64(binary.BigEndian.Uint64(hdr[8:16]))
			hdrLen = 16
		} else if boxSize == 0 {
			boxSize = end - offset
		}
		if boxSize < hdrLen || offset+boxSize > end {
			break
		}
		payloadStart := offset + hdrLen
		payloadEnd := offset + boxSize

		switch boxType {
		case "moov", "udta", "ilst":
			if s, _ := extractMP4Lyrics(f, payloadStart, payloadEnd, depth+1); s != "" {
				return s, nil
			}
		case "meta":
			// ISO/IEC 14496-12 FullBox: 4-byte version/flags before child boxes
			if payloadStart+4 < payloadEnd {
				if s, _ := extractMP4Lyrics(f, payloadStart+4, payloadEnd, depth+1); s != "" {
					return s, nil
				}
			}
		case "\xa9lyr":
			if s := readMP4DataBoxText(f, payloadStart, payloadEnd); s != "" {
				return s, nil
			}
		}
		offset += boxSize
	}
	return "", nil
}

func readMP4DataBoxText(f *os.File, start, end int64) string {
	var hdr [16]byte
	offset := start
	for offset+8 <= end {
		if _, err := f.ReadAt(hdr[:8], offset); err != nil {
			break
		}
		boxSize := int64(binary.BigEndian.Uint32(hdr[0:4]))
		boxType := string(hdr[4:8])
		hdrLen := int64(8)
		if boxSize == 1 {
			if offset+16 > end {
				break
			}
			if _, err := f.ReadAt(hdr[8:16], offset+8); err != nil {
				break
			}
			boxSize = int64(binary.BigEndian.Uint64(hdr[8:16]))
			hdrLen = 16
		} else if boxSize == 0 {
			boxSize = end - offset
		}
		if boxSize < hdrLen || offset+boxSize > end {
			break
		}
		if boxType == "data" {
			// iTunes metadata data atom has 8 bytes (4B type indicator + 4B locale) after header
			dataOffset := offset + hdrLen + 8
			dataLen := (offset + boxSize) - dataOffset
			if dataLen > 0 && dataLen <= maxEmbeddedTagBlock {
				buf := make([]byte, dataLen)
				if _, err := f.ReadAt(buf, dataOffset); err == nil {
					return strings.TrimSpace(strings.TrimRight(string(buf), "\x00"))
				}
			}
		}
		offset += boxSize
	}
	return ""
}
