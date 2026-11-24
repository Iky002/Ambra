package scanner

import (
	"strings"
	"testing"
)

func TestTradToSimp(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"流浪地球", "流浪地球"},
		{"盜夢空間", "盗梦空间"},
		{"進擊的巨人", "进击的巨人"},
		{"霸王別姬", "霸王别姬"},
		{"星際穿越 電影", "星际穿越 电影"},
		{"三體 3 Body", "三体 3 Body"},
		{"無間道 復仇者聯盟", "无间道 复仇者联盟"},
		{"銀魂 龍珠 鐵血戰士", "银魂 龙珠 铁血战士"},
		{"Inception 2010", "Inception 2010"},
	}

	for _, tc := range cases {
		if got := TradToSimp(tc.in); got != tc.want {
			t.Errorf("TradToSimp(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestToPinyin(t *testing.T) {
	t.Run("pure simplified Chinese", func(t *testing.T) {
		full, initials := ToPinyin("流浪地球")
		if full != "liu lang di qiu liulangdiqiu" {
			t.Fatalf("full = %q, want %q", full, "liu lang di qiu liulangdiqiu")
		}
		if initials != "lldq" {
			t.Fatalf("initials = %q, want %q", initials, "lldq")
		}
	})

	t.Run("traditional Chinese normalized before pinyin", func(t *testing.T) {
		full, initials := ToPinyin("盜夢空間")
		if full != "dao meng kong jian daomengkongjian" {
			t.Fatalf("full = %q, want %q", full, "dao meng kong jian daomengkongjian")
		}
		if initials != "dmkj" {
			t.Fatalf("initials = %q, want %q", initials, "dmkj")
		}
	})

	t.Run("mixed Chinese and trailing digit", func(t *testing.T) {
		full, initials := ToPinyin("流浪地球2")
		for _, want := range []string{"liu", "lang", "di", "qiu", "liulangdiqiu", "2", "liulangdiqiu2"} {
			if !strings.Contains(full, want) {
				t.Errorf("full %q missing %q", full, want)
			}
		}
		for _, want := range []string{"lldq", "2", "lldq2"} {
			if !strings.Contains(initials, want) {
				t.Errorf("initials %q missing %q", initials, want)
			}
		}
	})

	t.Run("mixed Chinese English and digits", func(t *testing.T) {
		full, initials := ToPinyin("三体 3 Body")
		if full != "san ti santi 3 body" {
			t.Fatalf("full = %q, want %q", full, "san ti santi 3 body")
		}
		if initials != "st 3 body" {
			t.Fatalf("initials = %q, want %q", initials, "st 3 body")
		}
	})

	t.Run("empty and punctuation", func(t *testing.T) {
		if full, init := ToPinyin("   "); full != "" || init != "" {
			t.Fatalf("expected empty, got full=%q init=%q", full, init)
		}
		if full, init := ToPinyin("《霸王別姬》!"); !strings.Contains(full, "bawangbieji") || init != "bwbj" {
			t.Fatalf("unexpected punctuation result: full=%q init=%q", full, init)
		}
	})
}
