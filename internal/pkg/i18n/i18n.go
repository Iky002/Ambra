package i18n

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// DefaultLocale is the fallback canonical locale used when no supported locale matches.
const DefaultLocale = "zh-CN"

// SupportedLocales lists all 10 canonical locales supported by Ambra.
var SupportedLocales = []string{
	"zh-CN",
	"zh-TW",
	"en-US",
	"ja-JP",
	"ko-KR",
	"es-ES",
	"de-DE",
	"fr-FR",
	"pt-BR",
	"th-TH",
}

type langRange struct {
	tag string
	q   float64
}

// ResolveLocale parses an HTTP Accept-Language header (respecting q= quality
// weights and prefix matching) and returns one of the 10 canonical locales.
// If acceptLanguage has no match, fallback is normalized and used if supported;
// otherwise DefaultLocale ("zh-CN") is returned.
func ResolveLocale(acceptLanguage, fallback string) string {
	if s := strings.TrimSpace(acceptLanguage); s != "" {
		parts := strings.Split(s, ",")
		ranges := make([]langRange, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			segs := strings.Split(part, ";")
			tag := strings.TrimSpace(segs[0])
			if tag == "" {
				continue
			}
			q := 1.0
			validQ := true
			for _, param := range segs[1:] {
				param = strings.TrimSpace(param)
				if len(param) >= 2 && (param[0] == 'q' || param[0] == 'Q') && param[1] == '=' {
					v, err := strconv.ParseFloat(strings.TrimSpace(param[2:]), 64)
					if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
						validQ = false
						break
					}
					q = v
				}
			}
			if !validQ || q == 0 {
				continue
			}
			ranges = append(ranges, langRange{tag: tag, q: q})
		}

		sort.SliceStable(ranges, func(i, j int) bool {
			return ranges[i].q > ranges[j].q
		})

		for _, r := range ranges {
			if loc, ok := matchLocale(r.tag); ok {
				return loc
			}
		}
	}

	if loc, ok := matchLocale(fallback); ok {
		return loc
	}
	return DefaultLocale
}

func matchLocale(tag string) (string, bool) {
	norm := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(tag), "_", "-"))
	if norm == "" || norm == "*" {
		return "", false
	}

	// Traditional Chinese variants take precedence over generic zh*.
	if hasLangPrefix(norm, "zh-tw") ||
		hasLangPrefix(norm, "zh-hk") ||
		hasLangPrefix(norm, "zh-mo") ||
		hasLangPrefix(norm, "zh-hant") {
		return "zh-TW", true
	}

	primary := norm
	if idx := strings.IndexByte(norm, '-'); idx >= 0 {
		primary = norm[:idx]
	}

	switch primary {
	case "zh":
		return "zh-CN", true
	case "en":
		return "en-US", true
	case "ja":
		return "ja-JP", true
	case "ko":
		return "ko-KR", true
	case "es":
		return "es-ES", true
	case "de":
		return "de-DE", true
	case "fr":
		return "fr-FR", true
	case "pt":
		return "pt-BR", true
	case "th":
		return "th-TH", true
	default:
		return "", false
	}
}

func hasLangPrefix(norm, prefix string) bool {
	if norm == prefix {
		return true
	}
	return strings.HasPrefix(norm, prefix+"-")
}

// T returns the localized string for the given locale and message key.
// Unknown locales fall back via ResolveLocale; unknown keys return the key itself.
func T(locale, key string) string {
	loc := ResolveLocale(locale, DefaultLocale)
	if catalog, ok := catalogs[loc]; ok {
		if msg, ok := catalog[key]; ok {
			return msg
		}
	}
	if msg, ok := catalogs[DefaultLocale][key]; ok {
		return msg
	}
	return key
}

// AllKeys returns a sorted slice of all message keys defined in the default catalog.
func AllKeys() []string {
	def := catalogs[DefaultLocale]
	keys := make([]string, 0, len(def))
	for k := range def {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var catalogs = map[string]map[string]string{
	"zh-CN": {
		"task.scan_library.name":    "扫描媒体库",
		"task.scan_library.desc":    "扫描所有已配置的媒体库目录并更新媒体索引与元数据。",
		"task.clean_transcode.name": "清理转码缓存",
		"task.clean_transcode.desc": "清理过期的 HLS/FFmpeg 临时转码切片文件以释放磁盘空间。",
		"task.vacuum_db.name":       "优化 SQLite 数据库",
		"task.vacuum_db.desc":       "执行 SQLite WAL 检查点与统计信息优化，提升查询性能。",
		"task.sync_rankings.name":   "同步热门榜单",
		"task.sync_rankings.desc":   "从外部数据源同步最新影视热门榜单与评分数据。",
		"notif.library_updated.name": "媒体库已更新",
		"notif.library_updated.desc": "媒体库扫描已完成。",
		"notif.task_failed.name":     "计划任务执行失败",
		"notif.task_failed.desc":     "计划任务在执行过程中发生错误。",
	},
	"zh-TW": {
		"task.scan_library.name":    "掃描媒體庫",
		"task.scan_library.desc":    "掃描所有已設定的媒體庫目錄並更新媒體索引與中繼資料。",
		"task.clean_transcode.name": "清理轉碼快取",
		"task.clean_transcode.desc": "清理過期的 HLS/FFmpeg 暫存轉碼切片檔案以釋放磁碟空間。",
		"task.vacuum_db.name":       "最佳化 SQLite 資料庫",
		"task.vacuum_db.desc":       "執行 SQLite WAL 檢查點與統計資訊最佳化，提升查詢效能。",
		"task.sync_rankings.name":   "同步熱門榜單",
		"task.sync_rankings.desc":   "從外部資料來源同步最新影視熱門榜單與評分資料。",
		"notif.library_updated.name": "媒體庫已更新",
		"notif.library_updated.desc": "媒體庫掃描已完成。",
		"notif.task_failed.name":     "排程任務執行失敗",
		"notif.task_failed.desc":     "排程任務在執行過程中發生錯誤。",
	},
	"en-US": {
		"task.scan_library.name":    "Scan Media Library",
		"task.scan_library.desc":    "Scan all configured media library folders and update indexes and metadata.",
		"task.clean_transcode.name": "Clean Transcode Cache",
		"task.clean_transcode.desc": "Remove expired temporary HLS/FFmpeg transcode segments to free disk space.",
		"task.vacuum_db.name":       "Optimize SQLite Database",
		"task.vacuum_db.desc":       "Run SQLite WAL checkpoint and query planner optimization to improve performance.",
		"task.sync_rankings.name":   "Sync Trending Rankings",
		"task.sync_rankings.desc":   "Sync the latest trending media rankings and ratings from external sources.",
		"notif.library_updated.name": "Library Updated",
		"notif.library_updated.desc": "Media library scan completed.",
		"notif.task_failed.name":     "Scheduled Task Failed",
		"notif.task_failed.desc":     "A scheduled task encountered an error during execution.",
	},
	"ja-JP": {
		"task.scan_library.name":    "メディアライブラリをスキャン",
		"task.scan_library.desc":    "設定済みのすべてのメディアライブラリフォルダをスキャンし、インデックスとメタデータを更新します。",
		"task.clean_transcode.name": "トランスコードキャッシュを削除",
		"task.clean_transcode.desc": "期限切れの HLS/FFmpeg 一時トランスコードファイルを削除してディスク容量を解放します。",
		"task.vacuum_db.name":       "SQLite データベースを最適化",
		"task.vacuum_db.desc":       "SQLite の WAL チェックポイントと統計情報の最適化を実行し、クエリ性能を向上させます。",
		"task.sync_rankings.name":   "人気ランキングを同期",
		"task.sync_rankings.desc":   "外部データソースから最新の人気ランキングと評価データを同期します。",
		"notif.library_updated.name": "ライブラリが更新されました",
		"notif.library_updated.desc": "メディアライブラリのスキャンが完了しました。",
		"notif.task_failed.name":     "スケジュールタスクが失敗しました",
		"notif.task_failed.desc":     "スケジュールタスクの実行中にエラーが発生しました。",
	},
	"ko-KR": {
		"task.scan_library.name":    "미디어 라이브러리 스캔",
		"task.scan_library.desc":    "구성된 모든 미디어 라이브러리 폴더를 스캔하고 인덱스와 메타데이터를 업데이트합니다.",
		"task.clean_transcode.name": "트랜스코드 캐시 정리",
		"task.clean_transcode.desc": "만료된 HLS/FFmpeg 임시 트랜스코드 파일을 삭제하여 디스크 공간을 확보합니다.",
		"task.vacuum_db.name":       "SQLite 데이터베이스 최적화",
		"task.vacuum_db.desc":       "SQLite WAL 체크포인트와 통계 최적화를 실행하여 쿼리 성능을 향상시킵니다.",
		"task.sync_rankings.name":   "인기 순위 동기화",
		"task.sync_rankings.desc":   "외부 데이터 소스에서 최신 인기 순위와 평점 데이터를 동기화합니다.",
		"notif.library_updated.name": "라이브러리가 업데이트됨",
		"notif.library_updated.desc": "미디어 라이브러리 스캔이 완료되었습니다.",
		"notif.task_failed.name":     "예약 작업 실패",
		"notif.task_failed.desc":     "예약 작업을 실행하는 동안 오류가 발생했습니다.",
	},
	"es-ES": {
		"task.scan_library.name":    "Escanear biblioteca multimedia",
		"task.scan_library.desc":    "Escanea todas las carpetas configuradas de la biblioteca y actualiza los índices y metadatos.",
		"task.clean_transcode.name": "Limpiar caché de transcodificación",
		"task.clean_transcode.desc": "Elimina los segmentos temporales caducados de HLS/FFmpeg para liberar espacio en disco.",
		"task.vacuum_db.name":       "Optimizar base de datos SQLite",
		"task.vacuum_db.desc":       "Ejecuta el punto de control WAL y la optimización de estadísticas de SQLite para mejorar el rendimiento.",
		"task.sync_rankings.name":   "Sincronizar clasificaciones populares",
		"task.sync_rankings.desc":   "Sincroniza las clasificaciones y puntuaciones más recientes desde fuentes externas.",
		"notif.library_updated.name": "Biblioteca actualizada",
		"notif.library_updated.desc": "Se completó el escaneo de la biblioteca multimedia.",
		"notif.task_failed.name":     "Error en la tarea programada",
		"notif.task_failed.desc":     "Se produjo un error durante la ejecución de una tarea programada.",
	},
	"de-DE": {
		"task.scan_library.name":    "Mediathek scannen",
		"task.scan_library.desc":    "Alle konfigurierten Mediathek-Ordner scannen und Indizes sowie Metadaten aktualisieren.",
		"task.clean_transcode.name": "Transkodierungs-Cache bereinigen",
		"task.clean_transcode.desc": "Abgelaufene temporäre HLS/FFmpeg-Transkodierungssegmente entfernen, um Speicherplatz freizugeben.",
		"task.vacuum_db.name":       "SQLite-Datenbank optimieren",
		"task.vacuum_db.desc":       "SQLite-WAL-Checkpoint und Statistikoptimierung ausführen, um die Abfrageleistung zu verbessern.",
		"task.sync_rankings.name":   "Beliebte Ranglisten synchronisieren",
		"task.sync_rankings.desc":   "Aktuelle Ranglisten und Bewertungen aus externen Datenquellen synchronisieren.",
		"notif.library_updated.name": "Mediathek aktualisiert",
		"notif.library_updated.desc": "Der Scan der Mediathek wurde abgeschlossen.",
		"notif.task_failed.name":     "Geplante Aufgabe fehlgeschlagen",
		"notif.task_failed.desc":     "Bei der Ausführung einer geplanten Aufgabe ist ein Fehler aufgetreten.",
	},
	"fr-FR": {
		"task.scan_library.name":    "Analyser la médiathèque",
		"task.scan_library.desc":    "Analyse tous les dossiers de la médiathèque configurés et met à jour les index et métadonnées.",
		"task.clean_transcode.name": "Nettoyer le cache de transcodage",
		"task.clean_transcode.desc": "Supprime les segments temporaires HLS/FFmpeg expirés pour libérer de l'espace disque.",
		"task.vacuum_db.name":       "Optimiser la base de données SQLite",
		"task.vacuum_db.desc":       "Exécute un point de contrôle WAL et optimise les statistiques SQLite pour améliorer les performances.",
		"task.sync_rankings.name":   "Synchroniser les classements",
		"task.sync_rankings.desc":   "Synchronise les derniers classements et notes depuis les sources externes.",
		"notif.library_updated.name": "Médiathèque mise à jour",
		"notif.library_updated.desc": "L'analyse de la médiathèque est terminée.",
		"notif.task_failed.name":     "Échec de la tâche planifiée",
		"notif.task_failed.desc":     "Une erreur s'est produite lors de l'exécution d'une tâche planifiée.",
	},
	"pt-BR": {
		"task.scan_library.name":    "Escanear biblioteca de mídia",
		"task.scan_library.desc":    "Escaneia todas as pastas configuradas da biblioteca e atualiza índices e metadados.",
		"task.clean_transcode.name": "Limpar cache de transcodificação",
		"task.clean_transcode.desc": "Remove segmentos temporários expirados de HLS/FFmpeg para liberar espaço em disco.",
		"task.vacuum_db.name":       "Otimizar banco de dados SQLite",
		"task.vacuum_db.desc":       "Executa o checkpoint WAL e a otimização de estatísticas do SQLite para melhorar o desempenho.",
		"task.sync_rankings.name":   "Sincronizar rankings populares",
		"task.sync_rankings.desc":   "Sincroniza os rankings e avaliações mais recentes de fontes externas.",
		"notif.library_updated.name": "Biblioteca atualizada",
		"notif.library_updated.desc": "A verificação da biblioteca de mídia foi concluída.",
		"notif.task_failed.name":     "Falha na tarefa agendada",
		"notif.task_failed.desc":     "Ocorreu um erro durante a execução de uma tarefa agendada.",
	},
	"th-TH": {
		"task.scan_library.name":    "สแกนคลังสื่อ",
		"task.scan_library.desc":    "สแกนโฟลเดอร์คลังสื่อที่กำหนดค่าไว้ทั้งหมดและอัปเดตดัชนีกับเมตาดาต้า",
		"task.clean_transcode.name": "ล้างแคชการแปลงไฟล์",
		"task.clean_transcode.desc": "ลบไฟล์ชั่วคราวของ HLS/FFmpeg ที่หมดอายุเพื่อเพิ่มพื้นที่ดิสก์",
		"task.vacuum_db.name":       "ปรับแต่งฐานข้อมูล SQLite",
		"task.vacuum_db.desc":       "ดำเนินการ SQLite WAL checkpoint และปรับแต่งสถิติเพื่อเพิ่มประสิทธิภาพการค้นหา",
		"task.sync_rankings.name":   "ซิงก์อันดับยอดนิยม",
		"task.sync_rankings.desc":   "ซิงก์อันดับสื่อยอดนิยมและคะแนนล่าสุดจากแหล่งข้อมูลภายนอก",
		"notif.library_updated.name": "อัปเดตคลังสื่อแล้ว",
		"notif.library_updated.desc": "การสแกนคลังสื่อเสร็จสมบูรณ์แล้ว",
		"notif.task_failed.name":     "งานตามกำหนดเวลาล้มเหลว",
		"notif.task_failed.desc":     "เกิดข้อผิดพลาดระหว่างการทำงานของงานตามกำหนดเวลา",
	},
}
