package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTradToSimpAndToPinyinUnit(t *testing.T) {
	if got := TradToSimp(""); got != "" {
		t.Fatalf("TradToSimp empty = %q", got)
	}
	if got := TradToSimp("盜夢空間 進擊的巨人 霸王別姬"); got != "盗梦空间 进击的巨人 霸王别姬" {
		t.Fatalf("TradToSimp = %q", got)
	}

	full, initials := ToPinyin("流浪地球")
	if full != "liu lang di qiu liulangdiqiu" || initials != "lldq" {
		t.Fatalf("ToPinyin(流浪地球) = (%q, %q)", full, initials)
	}

	// Single CJK char + duplicate tokens + punctuation-only
	full1, init1 := ToPinyin("愛 愛")
	if full1 != "ai" || init1 != "ai" && init1 != "a" {
		t.Fatalf("ToPinyin(愛 愛) = (%q, %q)", full1, init1)
	}
	if f, in := ToPinyin("  !!!  "); f != "" || in != "" {
		t.Fatalf("ToPinyin(punct) = (%q, %q)", f, in)
	}
	if f, in := ToPinyin(""); f != "" || in != "" {
		t.Fatalf("ToPinyin(empty) = (%q, %q)", f, in)
	}

	// Mixed Chinese + English + digits
	fMix, iMix := ToPinyin("三体 3 Body")
	if fMix != "san ti santi 3 body" || iMix != "st 3 body" {
		t.Fatalf("ToPinyin(三体 3 Body) = (%q, %q)", fMix, iMix)
	}
	fMix2, iMix2 := ToPinyin("流浪地球2")
	if !strings.Contains(fMix2, "liulangdiqiu") || !strings.Contains(fMix2, "2") || !strings.Contains(iMix2, "lldq2") {
		t.Fatalf("ToPinyin(流浪地球2) = (%q, %q)", fMix2, iMix2)
	}

	// tokenizeForFTSSQLite helper coverage
	toks := tokenizeForFTSSQLite("《流浪地球2》 The Wandering Earth")
	for _, want := range []string{"流浪", "地球", "2", "the", "wandering", "earth"} {
		if !strings.Contains(toks, want) {
			t.Errorf("tokenizeForFTSSQLite missing %q in %q", want, toks)
		}
	}
}

func TestChinesePinyinAndTraditionalFTSSearch(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pinyin_search.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	repo := NewLibraryRepo(st)
	lib, err := repo.CreateLibrary(ctx, "电影", "movies", []string{"/movies"})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	movies := []struct {
		name        string
		origTitle   string
		people      string
		titleTokens string // leave empty on some to exercise auto-tokenization too
	}{
		{
			name:        "流浪地球",
			origTitle:   "The Wandering Earth",
			people:      "吴京 郭帆",
			titleTokens: "流浪地球 流 浪 地 球 流浪 浪地 地球 the wandering earth",
		},
		{
			name:        "盗梦空间",
			origTitle:   "Inception",
			people:      "莱昂纳多",
			titleTokens: "", // auto-tokenized by UpsertSearchFTS
		},
		{
			name:        "進擊的巨人", // stored in Traditional Chinese!
			origTitle:   "Attack on Titan",
			people:      "諫山創",
			titleTokens: "進擊的巨人 進 擊 的 巨 人 進擊 擊的 的巨 巨人 attack on titan",
		},
		{
			name:        "霸王别姬",
			origTitle:   "Farewell My Concubine",
			people:      "張國榮 陈凯歌",
			titleTokens: "",
		},
		{
			name:        "流浪地球2",
			origTitle:   "The Wandering Earth II",
			people:      "吴京 刘德华",
			titleTokens: "",
		},
	}

	idsByName := make(map[string]string, len(movies))
	for _, m := range movies {
		rec := &ItemRecord{
			LibraryID:     lib.ID,
			ParentID:      lib.ID,
			Type:          "Movie",
			MediaType:     "Video",
			Name:          m.name,
			OriginalTitle: m.origTitle,
		}
		if err := repo.UpsertItem(ctx, rec); err != nil {
			t.Fatalf("UpsertItem(%s): %v", m.name, err)
		}
		idsByName[m.name] = rec.ID

		// Deliberately pass empty pinyinFull and pinyinInitials to verify automatic population
		titleRaw := strings.TrimSpace(m.name + " " + m.origTitle)
		if err := repo.UpsertSearchFTS(ctx, rec.ID, titleRaw, m.titleTokens, "", "", m.people); err != nil {
			t.Fatalf("UpsertSearchFTS(%s): %v", m.name, err)
		}
	}

	queries := []struct {
		term     string
		wantName string
	}{
		// Initials
		{"lldq", "流浪地球"},
		{"lldq2", "流浪地球2"},
		{"dmkj", "盗梦空间"},
		{"jjdjr", "進擊的巨人"},
		{"bwbj", "霸王别姬"},
		// Full pinyin (joined and spaced)
		{"liulang", "流浪地球"},
		{"liulangdiqiu", "流浪地球"},
		{"liu lang", "流浪地球"},
		{"daomeng", "盗梦空间"},
		{"bawang", "霸王别姬"},
		// Out-of-order & exact Simplified Chinese
		{"流浪 地球", "流浪地球"},
		{"地球 流浪", "流浪地球"},
		{"流浪地球", "流浪地球"},
		// Traditional Chinese queries matching Simplified records
		{"盜夢空間", "盗梦空间"},
		{"霸王別姬", "霸王别姬"},
		// Both Simplified and Traditional queries matching a Traditional-stored record
		{"進擊的巨人", "進擊的巨人"},
		{"进击的巨人", "進擊的巨人"},
		// Cast pinyin & Traditional->Simplified cast query
		{"wujing", "流浪地球"},
		{"张国荣", "霸王别姬"},
		{"張國榮", "霸王别姬"},
		{"zgr", "霸王别姬"},
	}

	for _, q := range queries {
		start := time.Now()
		items, total, err := repo.QueryItems(ctx, ItemFilter{
			LibraryID:  lib.ID,
			SearchTerm: q.term,
			Limit:      10,
		})
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("QueryItems(%q) error: %v", q.term, err)
		}
		if total == 0 || len(items) == 0 {
			t.Fatalf("QueryItems(%q) returned 0 hits, want %q", q.term, q.wantName)
		}
		wantID := idsByName[q.wantName]
		found := false
		for _, it := range items {
			if it.Id == wantID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("QueryItems(%q) did not include %q (id=%s), got %d items", q.term, q.wantName, wantID, len(items))
		}
		if elapsed > 100*time.Millisecond {
			t.Errorf("QueryItems(%q) took %v, expected millisecond-level response", q.term, elapsed)
		}

		// Also verify SearchHints uses the same normalized FTS path
		hints, hintTotal, err := repo.SearchHints(ctx, SearchHintFilter{
			SearchTerm: q.term,
			Limit:      10,
		})
		if err != nil {
			t.Fatalf("SearchHints(%q) error: %v", q.term, err)
		}
		if hintTotal == 0 || len(hints) == 0 {
			t.Fatalf("SearchHints(%q) returned 0 hits, want %q", q.term, q.wantName)
		}
	}
}
