package scanner

import "github.com/ambra-media/ambra/internal/store/sqlite"

// TradToSimp normalizes Traditional Chinese characters in s to Simplified Chinese.
func TradToSimp(s string) string {
	return sqlite.TradToSimp(s)
}

// ToPinyin converts Chinese text into full pinyin tokens (space-separated syllables
// plus joined full pinyin) and contiguous initial letters, preserving English words
// and digits.
func ToPinyin(s string) (full string, initials string) {
	return sqlite.ToPinyin(s)
}
