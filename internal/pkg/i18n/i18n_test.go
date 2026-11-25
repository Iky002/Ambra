package i18n

import "testing"

func TestResolveLocale(t *testing.T) {
	tests := []struct {
		name           string
		acceptLanguage string
		fallback       string
		want           string
	}{
		{name: "empty header and fallback defaults to zh-CN", acceptLanguage: "", fallback: "", want: "zh-CN"},
		{name: "empty header uses fallback", acceptLanguage: "", fallback: "ja-JP", want: "ja-JP"},
		{name: "empty header normalizes fallback", acceptLanguage: "", fallback: "zh_HK", want: "zh-TW"},
		{name: "unmatched header uses fallback", acceptLanguage: "it-IT,ru-RU;q=0.9", fallback: "en", want: "en-US"},
		{name: "unmatched header and invalid fallback defaults to zh-CN", acceptLanguage: "it-IT", fallback: "xx-YY", want: "zh-CN"},
		{name: "wildcard ignored", acceptLanguage: "*", fallback: "fr", want: "fr-FR"},
		{name: "zh-TW exact", acceptLanguage: "zh-TW", want: "zh-TW"},
		{name: "zh-HK maps to zh-TW", acceptLanguage: "zh-HK", want: "zh-TW"},
		{name: "zh-MO maps to zh-TW", acceptLanguage: "zh-MO", want: "zh-TW"},
		{name: "zh-Hant maps to zh-TW", acceptLanguage: "zh-Hant", want: "zh-TW"},
		{name: "zh-Hant-TW maps to zh-TW", acceptLanguage: "zh-Hant-TW", want: "zh-TW"},
		{name: "zh-CN exact", acceptLanguage: "zh-CN", want: "zh-CN"},
		{name: "zh-Hans maps to zh-CN", acceptLanguage: "zh-Hans", want: "zh-CN"},
		{name: "zh generic maps to zh-CN", acceptLanguage: "zh", want: "zh-CN"},
		{name: "en-GB maps to en-US", acceptLanguage: "en-GB", want: "en-US"},
		{name: "ja maps to ja-JP", acceptLanguage: "ja", want: "ja-JP"},
		{name: "ko-KR exact", acceptLanguage: "ko-KR", want: "ko-KR"},
		{name: "es-MX maps to es-ES", acceptLanguage: "es-MX", want: "es-ES"},
		{name: "de-AT maps to de-DE", acceptLanguage: "de-AT", want: "de-DE"},
		{name: "fr-CA maps to fr-FR", acceptLanguage: "fr-CA", want: "fr-FR"},
		{name: "pt-PT maps to pt-BR", acceptLanguage: "pt-PT", want: "pt-BR"},
		{name: "th maps to th-TH", acceptLanguage: "th", want: "th-TH"},
		{name: "respects q weights", acceptLanguage: "en-US;q=0.5, ja-JP;q=0.9, de-DE;q=0.7", want: "ja-JP"},
		{name: "preserves order on equal q weights", acceptLanguage: "ko;q=0.8, fr;q=0.8", want: "ko-KR"},
		{name: "q=0 is ignored", acceptLanguage: "en-US;q=0, th-TH;q=0.5", want: "th-TH"},
		{name: "invalid q value is ignored", acceptLanguage: "en-US;q=abc, de-DE;q=1.5, pt-BR;q=0.4", want: "pt-BR"},
		{name: "uppercase Q parameter", acceptLanguage: "en-US;Q=0.3, es-ES;Q=0.8", want: "es-ES"},
		{name: "empty segments and spaces", acceptLanguage: " , ;q=0.9 ,  fr-FR ; q=0.8 , ", want: "fr-FR"},
		{name: "does not false-match non-language prefix", acceptLanguage: "theme, english", fallback: "ko", want: "ko-KR"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveLocale(tc.acceptLanguage, tc.fallback); got != tc.want {
				t.Fatalf("ResolveLocale(%q, %q) = %q, want %q", tc.acceptLanguage, tc.fallback, got, tc.want)
			}
		})
	}
}

func TestCatalogCompletenessAndT(t *testing.T) {
	expectedKeys := []string{
		"task.scan_library.name",
		"task.scan_library.desc",
		"task.clean_transcode.name",
		"task.clean_transcode.desc",
		"task.vacuum_db.name",
		"task.vacuum_db.desc",
		"task.sync_rankings.name",
		"task.sync_rankings.desc",
		"notif.library_updated.name",
		"notif.library_updated.desc",
		"notif.task_failed.name",
		"notif.task_failed.desc",
	}

	if len(SupportedLocales) != 10 {
		t.Fatalf("expected 10 supported locales, got %d", len(SupportedLocales))
	}

	allKeys := AllKeys()
	if len(allKeys) != len(expectedKeys) {
		t.Fatalf("AllKeys() count = %d, want %d", len(allKeys), len(expectedKeys))
	}

	for _, loc := range SupportedLocales {
		cat, ok := catalogs[loc]
		if !ok {
			t.Fatalf("missing catalog for locale %q", loc)
		}
		if len(cat) != len(expectedKeys) {
			t.Fatalf("locale %q has %d keys, want %d", loc, len(cat), len(expectedKeys))
		}
		for _, key := range expectedKeys {
			val := T(loc, key)
			if val == "" || val == key {
				t.Fatalf("T(%q, %q) returned untranslated/empty value %q", loc, key, val)
			}
		}
	}

	// Verify distinct translations across sample locales.
	if T("en-US", "task.scan_library.name") != "Scan Media Library" {
		t.Fatalf("unexpected en-US translation: %q", T("en-US", "task.scan_library.name"))
	}
	if T("ja-JP", "task.scan_library.name") != "メディアライブラリをスキャン" {
		t.Fatalf("unexpected ja-JP translation: %q", T("ja-JP", "task.scan_library.name"))
	}
	if T("zh-TW", "task.scan_library.name") != "掃描媒體庫" {
		t.Fatalf("unexpected zh-TW translation: %q", T("zh-TW", "task.scan_library.name"))
	}
	if T("th-TH", "task.scan_library.name") != "สแกนคลังสื่อ" {
		t.Fatalf("unexpected th-TH translation: %q", T("th-TH", "task.scan_library.name"))
	}

	// Unknown key returns key itself.
	if got := T("en-US", "unknown.missing.key"); got != "unknown.missing.key" {
		t.Fatalf("T unknown key = %q, want key itself", got)
	}

	// Unknown locale falls back to DefaultLocale (zh-CN).
	if got := T("xx-YY", "notif.library_updated.name"); got != T(DefaultLocale, "notif.library_updated.name") {
		t.Fatalf("T unknown locale = %q, want default %q", got, T(DefaultLocale, "notif.library_updated.name"))
	}

	// Fallback to DefaultLocale when a key is missing in a specific locale catalog.
	orig := catalogs["en-US"]["notif.task_failed.name"]
	delete(catalogs["en-US"], "notif.task_failed.name")
	defer func() { catalogs["en-US"]["notif.task_failed.name"] = orig }()
	if got := T("en-US", "notif.task_failed.name"); got != T(DefaultLocale, "notif.task_failed.name") {
		t.Fatalf("expected fallback to DefaultLocale, got %q", got)
	}
}
