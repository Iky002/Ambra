package tasks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ambra-media/ambra/internal/api/httpx"
	"github.com/ambra-media/ambra/internal/core/scheduler"
	"github.com/ambra-media/ambra/internal/pkg/i18n"
)

const (
	ticksPerDay        int64 = 86400 * 10_000_000
	minIntervalTicks   int64 = 60 * 10_000_000 // 1 minute
	maxTriggersPerTask       = 32
)

var extraTaskLocalizations = map[string]map[string][2]string{
	"ExtractChapterImages": {
		"zh-CN": {"提取章节缩略图", "为媒体库中的视频文件提取章节预览缩略图。"},
		"zh-TW": {"擷取章節縮圖", "為媒體庫中的視訊檔案擷取章節預覽縮圖。"},
		"en-US": {"Extract Chapter Images", "Extract chapter preview thumbnails for video files in the media library."},
		"ja-JP": {"チャプター画像を抽出", "メディアライブラリ内の動画ファイルのチャプターサムネイルを抽出します。"},
		"ko-KR": {"챕터 이미지 추출", "미디어 라이브러리의 비디오 파일에서 챕터 미리보기 썸네일을 추출합니다."},
		"es-ES": {"Extraer imágenes de capítulos", "Extrae miniaturas de vista previa de capítulos para los videos de la biblioteca."},
		"de-DE": {"Kapitelbilder extrahieren", "Extrahiert Kapitel-Vorschaubilder für Videodateien in der Mediathek."},
		"fr-FR": {"Extraire les images de chapitres", "Extrait les vignettes d'aperçu des chapitres pour les vidéos de la médiathèque."},
		"pt-BR": {"Extrair imagens de capítulos", "Extrai miniaturas de pré-visualização de capítulos para os vídeos da biblioteca."},
		"th-TH": {"ดึงภาพตัวอย่างบท", "ดึงภาพขนาดย่อของบทสำหรับไฟล์วิดีโอในคลังสื่อ"},
	},
	"GenerateBifThumbnails": {
		"zh-CN": {"生成进度条缩略图 (BIF)", "为媒体库中的视频生成进度条拖动预览缩略图索引文件。"},
		"zh-TW": {"產生進度條縮圖 (BIF)", "為媒體庫中的視訊產生進度條拖曳預覽縮圖索引檔案。"},
		"en-US": {"Generate BIF Thumbnails", "Generate seek-bar preview thumbnail index (BIF) files for videos in the library."},
		"ja-JP": {"BIF サムネイルを生成", "ライブラリ内の動画のシークバープレビュー用 BIF サムネイルを生成します。"},
		"ko-KR": {"BIF 썸네일 생성", "라이브러리의 비디오에 대한 탐색 막대 미리보기 BIF 썸네일을 생성합니다."},
		"es-ES": {"Generar miniaturas BIF", "Genera archivos de miniaturas BIF de vista previa para los videos de la biblioteca."},
		"de-DE": {"BIF-Vorschaubilder erstellen", "Erstellt BIF-Vorschaubilder für die Zeitleiste von Videos in der Mediathek."},
		"fr-FR": {"Générer les vignettes BIF", "Génère les fichiers de vignettes BIF d'aperçu pour les vidéos de la médiathèque."},
		"pt-BR": {"Gerar miniaturas BIF", "Gera arquivos de miniaturas BIF de pré-visualização para os vídeos da biblioteca."},
		"th-TH": {"สร้างภาพขนาดย่อ BIF", "สร้างไฟล์ภาพขนาดย่อ BIF สำหรับแสดงตัวอย่างแถบเวลาของวิดีโอในคลังสื่อ"},
	},
	"RefreshGuide": {
		"zh-CN": {"刷新节目指南", "从已配置的 EPG 源更新直播电视节目指南数据。"},
		"zh-TW": {"重新整理節目指南", "從已設定的 EPG 來源更新直播電視節目指南資料。"},
		"en-US": {"Refresh Guide", "Update Live TV electronic program guide (EPG) listings from configured guide providers."},
		"ja-JP": {"番組表を更新", "設定済みの EPG プロバイダーからライブ TV 番組表データを更新します。"},
		"ko-KR": {"편성표 새로고침", "구성된 EPG 공급자에서 라이브 TV 편성표 데이터를 업데이트합니다."},
		"es-ES": {"Actualizar guía", "Actualiza la guía de programación (EPG) de TV en vivo desde los proveedores configurados."},
		"de-DE": {"Programmführer aktualisieren", "Aktualisiert die Live-TV-Programmdaten (EPG) aus den konfigurierten Quellen."},
		"fr-FR": {"Actualiser le guide", "Met à jour le guide des programmes (EPG) de la TV en direct depuis les fournisseurs configurés."},
		"pt-BR": {"Atualizar guia", "Atualiza o guia de programação (EPG) da TV ao vivo a partir dos provedores configurados."},
		"th-TH": {"รีเฟรชผังรายการ", "อัปเดตข้อมูลผังรายการทีวีสด (EPG) จากผู้ให้บริการที่ตั้งค่าไว้"},
	},
	"RefreshTunerHosts": {
		"zh-CN": {"刷新调谐器频道", "重新同步已配置的 M3U 和 HDHomeRun 调谐器频道列表。"},
		"zh-TW": {"重新整理調諧器頻道", "重新同步已設定的 M3U 與 HDHomeRun 調諧器頻道清單。"},
		"en-US": {"Refresh Tuner Hosts", "Resynchronize channels from configured M3U and HDHomeRun tuner hosts."},
		"ja-JP": {"チューナーホストを更新", "設定済みの M3U および HDHomeRun チューナーホストのチャンネルを再同期します。"},
		"ko-KR": {"튜너 호스트 새로고침", "구성된 M3U 및 HDHomeRun 튜너 호스트의 채널을 다시 동기화합니다."},
		"es-ES": {"Actualizar sintonizadores", "Resincroniza los canales de los sintonizadores M3U y HDHomeRun configurados."},
		"de-DE": {"Tuner-Hosts aktualisieren", "Synchronisiert Kanäle der konfigurierten M3U- und HDHomeRun-Tuner neu."},
		"fr-FR": {"Actualiser les tuners", "Resynchronise les chaînes des tuners M3U et HDHomeRun configurés."},
		"pt-BR": {"Atualizar sintonizadores", "Ressincroniza os canais dos sintonizadores M3U e HDHomeRun configurados."},
		"th-TH": {"รีเฟรชโฮสต์จูนเนอร์", "ซิงโครไนซ์ช่องรายการจากโฮสต์จูนเนอร์ M3U และ HDHomeRun ที่ตั้งค่าไว้ใหม่"},
	},
}

var taskCategoryLocalizations = map[string]map[string]string{
	"library": {
		"zh-CN": "媒体库",
		"zh-TW": "媒體庫",
		"en-US": "Library",
		"ja-JP": "ライブラリ",
		"ko-KR": "라이브러리",
		"es-ES": "Biblioteca",
		"de-DE": "Mediathek",
		"fr-FR": "Médiathèque",
		"pt-BR": "Biblioteca",
		"th-TH": "คลังสื่อ",
	},
	"maintenance": {
		"zh-CN": "维护",
		"zh-TW": "維護",
		"en-US": "Maintenance",
		"ja-JP": "メンテナンス",
		"ko-KR": "유지 관리",
		"es-ES": "Mantenimiento",
		"de-DE": "Wartung",
		"fr-FR": "Maintenance",
		"pt-BR": "Manutenção",
		"th-TH": "การบำรุงรักษา",
	},
	"livetv": {
		"zh-CN": "直播电视",
		"zh-TW": "直播電視",
		"en-US": "Live TV",
		"ja-JP": "ライブ TV",
		"ko-KR": "라이브 TV",
		"es-ES": "TV en vivo",
		"de-DE": "Live-TV",
		"fr-FR": "TV en direct",
		"pt-BR": "TV ao vivo",
		"th-TH": "ทีวีสด",
	},
}

func localizeTaskCategory(category, locale string) string {
	norm := strings.ToLower(strings.TrimSpace(category))
	var canonical string
	switch norm {
	case "library", "媒体库", "媒體庫":
		canonical = "library"
	case "maintenance", "维护", "維護":
		canonical = "maintenance"
	case "livetv", "live tv", "直播电视", "直播電視", "电视直播":
		canonical = "livetv"
	default:
		return category
	}
	byLocale := taskCategoryLocalizations[canonical]
	if localized, ok := byLocale[locale]; ok {
		return localized
	}
	return byLocale["en-US"]
}

// AuthFuncs decouples route registration from the concrete middleware package.
type AuthFuncs struct {
	RequireAuth   func(http.Handler) http.Handler
	RequireAdmin  func(http.Handler) http.Handler
	UserIDFromCtx func(context.Context) string
}

type Handler struct {
	mgr  *scheduler.Manager
	auth AuthFuncs
}

func New(mgr *scheduler.Manager) *Handler {
	return &Handler{mgr: mgr}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth AuthFuncs) {
	h.auth = auth
	wrapAuth := auth.RequireAuth
	if wrapAuth == nil {
		wrapAuth = passthrough
	}
	wrapAdmin := auth.RequireAdmin
	if wrapAdmin == nil {
		wrapAdmin = wrapAuth
	}

	handleAdmin := func(pattern string, fn http.HandlerFunc) {
		mux.Handle(pattern, wrapAdmin(fn))
	}

	handleAdmin("GET /ScheduledTasks", h.handleListTasks)
	handleAdmin("GET /ScheduledTasks/{Id}", h.handleGetTask)
	// Dispatch both "POST /ScheduledTasks/Running/{Id}" and "POST /ScheduledTasks/{Id}/Triggers"
	// through a single pattern to avoid net/http.ServeMux precedence conflict on /ScheduledTasks/Running/Triggers.
	handleAdmin("POST /ScheduledTasks/{Seg1}/{Seg2}", h.handlePostDispatch)
	handleAdmin("DELETE /ScheduledTasks/Running/{Id}", h.handleStopTask)
}

func (h *Handler) handleListTasks(w http.ResponseWriter, r *http.Request) {
	q := httpx.ParseQuery(r)
	isHidden := q.Bool("IsHidden")
	isEnabled := q.Bool("IsEnabled")
	acceptLang := strings.TrimSpace(r.Header.Get("Accept-Language"))
	locale := ""
	if acceptLang != "" {
		locale = i18n.ResolveLocale(acceptLang, "")
	}

	all := h.mgr.List(true)
	filtered := make([]scheduler.TaskInfo, 0, len(all))
	for _, t := range all {
		if isHidden != nil {
			if t.IsHidden != *isHidden {
				continue
			}
		} else if t.IsHidden {
			continue
		}
		if isEnabled != nil {
			enabled := len(t.Triggers) > 0
			if enabled != *isEnabled {
				continue
			}
		}
		if locale != "" {
			t = localizeTaskInfo(t, locale)
		}
		filtered = append(filtered, t)
	}
	httpx.JSON(w, http.StatusOK, filtered)
}

func (h *Handler) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	info, ok := h.mgr.Get(id)
	if !ok {
		httpx.Error(w, http.StatusNotFound, "scheduled task not found")
		return
	}
	if acceptLang := strings.TrimSpace(r.Header.Get("Accept-Language")); acceptLang != "" {
		info = localizeTaskInfo(info, i18n.ResolveLocale(acceptLang, ""))
	}
	httpx.JSON(w, http.StatusOK, info)
}

func localizeTaskInfo(info scheduler.TaskInfo, locale string) scheduler.TaskInfo {
	if info.Category != "" {
		info.Category = localizeTaskCategory(info.Category, locale)
	}
	key := info.Key
	if key == "" {
		key = info.Id
	}
	var nameKey, descKey string
	switch key {
	case "RefreshLibrary":
		nameKey, descKey = "task.scan_library.name", "task.scan_library.desc"
	case "CleanTranscode", "CleanTranscodeCache":
		nameKey, descKey = "task.clean_transcode.name", "task.clean_transcode.desc"
	case "OptimizeDatabase":
		nameKey, descKey = "task.vacuum_db.name", "task.vacuum_db.desc"
	case "SyncRankings":
		nameKey, descKey = "task.sync_rankings.name", "task.sync_rankings.desc"
	case "ExtractChapterImages", "GenerateBifThumbnails", "RefreshGuide", "RefreshTunerHosts":
		if byLocale, ok := extraTaskLocalizations[key]; ok {
			pair, okLoc := byLocale[locale]
			if !okLoc {
				pair = byLocale["en-US"]
			}
			info.Name = pair[0]
			info.Description = pair[1]
		}
		return info
	default:
		return info
	}
	info.Name = i18n.T(locale, nameKey)
	info.Description = i18n.T(locale, descKey)
	return info
}

func (h *Handler) handlePostDispatch(w http.ResponseWriter, r *http.Request) {
	seg1 := r.PathValue("Seg1")
	seg2 := r.PathValue("Seg2")
	switch {
	case seg1 == "Running" && seg2 != "":
		h.runTaskByID(w, seg2)
	case seg2 == "Triggers" && seg1 != "":
		h.setTriggersByID(w, r, seg1)
	default:
		httpx.Error(w, http.StatusNotFound, "unknown scheduled task endpoint")
	}
}

func (h *Handler) runTaskByID(w http.ResponseWriter, id string) {
	if err := h.mgr.Run(id); err != nil {
		if errors.Is(err, scheduler.ErrTaskNotFound) {
			httpx.Error(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, scheduler.ErrTaskAlreadyRunning) {
			httpx.Error(w, http.StatusConflict, err.Error())
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) handleStopTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("Id")
	if err := h.mgr.Stop(id); err != nil {
		if errors.Is(err, scheduler.ErrTaskNotFound) {
			httpx.Error(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, scheduler.ErrTaskNotRunning) {
			httpx.Error(w, http.StatusConflict, err.Error())
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.NoContent(w)
}

func isValidDayOfWeek(day string) bool {
	switch strings.ToLower(strings.TrimSpace(day)) {
	case "sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday":
		return true
	default:
		return false
	}
}

func validateTriggers(triggers []scheduler.TaskTriggerInfo) error {
	if len(triggers) > maxTriggersPerTask {
		return fmt.Errorf("too many triggers: maximum %d allowed per task", maxTriggersPerTask)
	}
	for i, tr := range triggers {
		tType := strings.TrimSpace(tr.Type)
		switch tType {
		case "IntervalTrigger":
			if tr.IntervalTicks == nil {
				return fmt.Errorf("trigger[%d]: IntervalTicks is required for IntervalTrigger", i)
			}
		case "DailyTrigger":
			if tr.TimeOfDayTicks == nil {
				return fmt.Errorf("trigger[%d]: TimeOfDayTicks is required for DailyTrigger", i)
			}
		case "WeeklyTrigger":
			if tr.TimeOfDayTicks == nil {
				return fmt.Errorf("trigger[%d]: TimeOfDayTicks is required for WeeklyTrigger", i)
			}
			if !isValidDayOfWeek(tr.DayOfWeek) {
				return fmt.Errorf("trigger[%d]: valid DayOfWeek is required for WeeklyTrigger", i)
			}
		case "StartupTrigger", "SystemEventTrigger":
		default:
			return fmt.Errorf("trigger[%d]: unsupported trigger type %q", i, tr.Type)
		}
		if strings.TrimSpace(tr.DayOfWeek) != "" && !isValidDayOfWeek(tr.DayOfWeek) {
			return fmt.Errorf("trigger[%d]: invalid DayOfWeek %q", i, tr.DayOfWeek)
		}
		if tr.TimeOfDayTicks != nil && (*tr.TimeOfDayTicks < 0 || *tr.TimeOfDayTicks >= ticksPerDay) {
			return fmt.Errorf("trigger[%d]: TimeOfDayTicks must be between 0 and %d", i, ticksPerDay-1)
		}
		if tr.IntervalTicks != nil && *tr.IntervalTicks < minIntervalTicks {
			return fmt.Errorf("trigger[%d]: IntervalTicks must be at least %d (1 minute)", i, minIntervalTicks)
		}
		if tr.MaxRuntimeTicks != nil && *tr.MaxRuntimeTicks < 0 {
			return fmt.Errorf("trigger[%d]: MaxRuntimeTicks must be non-negative", i)
		}
	}
	return nil
}

func (h *Handler) setTriggersByID(w http.ResponseWriter, r *http.Request, id string) {
	var triggers []scheduler.TaskTriggerInfo
	if err := httpx.DecodeJSONLimit(r, &triggers, 0); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid triggers payload")
		return
	}
	if err := validateTriggers(triggers); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.mgr.SetTriggers(id, triggers); err != nil {
		if errors.Is(err, scheduler.ErrTaskNotFound) {
			httpx.Error(w, http.StatusNotFound, err.Error())
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.NoContent(w)
}

func passthrough(next http.Handler) http.Handler { return next }
