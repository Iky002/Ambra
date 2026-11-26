package tasks

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambra-media/ambra/internal/core/scheduler"
)

func TestTasksV0107AuditTriggerValidationAndLocalization(t *testing.T) {
	mgr := scheduler.New(nil)
	mgr.Register(scheduler.TaskDefinition{
		ID:          "ExtractChapterImages",
		Key:         "ExtractChapterImages",
		Name:        "Extract Chapter Images",
		Description: "Default desc",
	})
	mgr.Register(scheduler.TaskDefinition{
		ID:          "GenerateBifThumbnails",
		Key:         "GenerateBifThumbnails",
		Name:        "Generate BIF Thumbnails",
		Description: "Default bif desc",
	})

	mux := http.NewServeMux()
	h := New(mgr)
	h.RegisterRoutes(mux, AuthFuncs{})

	// 1. M-19: validateTriggers strict checks
	tooShort := int64(30 * 10_000_000) // 30s < 1min
	validInt := int64(60 * 10_000_000) // 1min
	validTOD := int64(3600 * 10_000_000)
	negRuntime := int64(-1)

	badPayloads := [][]scheduler.TaskTriggerInfo{
		// Missing IntervalTicks on IntervalTrigger
		{{Type: "IntervalTrigger"}},
		// IntervalTicks < 1 minute
		{{Type: "IntervalTrigger", IntervalTicks: &tooShort}},
		// Missing TimeOfDayTicks on DailyTrigger
		{{Type: "DailyTrigger"}},
		// Missing TimeOfDayTicks on WeeklyTrigger
		{{Type: "WeeklyTrigger", DayOfWeek: "Monday"}},
		// Missing or invalid DayOfWeek on WeeklyTrigger
		{{Type: "WeeklyTrigger", TimeOfDayTicks: &validTOD}},
		{{Type: "WeeklyTrigger", TimeOfDayTicks: &validTOD, DayOfWeek: "Funday"}},
		// Invalid DayOfWeek on StartupTrigger
		{{Type: "StartupTrigger", DayOfWeek: "InvalidDay"}},
		// Negative MaxRuntimeTicks
		{{Type: "StartupTrigger", MaxRuntimeTicks: &negRuntime}},
	}
	for i, triggers := range badPayloads {
		raw, _ := json.Marshal(triggers)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ScheduledTasks/ExtractChapterImages/Triggers", bytes.NewReader(raw)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("badPayloads[%d]: expected 400, got %d (%s)", i, rec.Code, rec.Body.String())
		}
	}

	// Exceeding maxTriggersPerTask (33 triggers) -> 400
	tooMany := make([]scheduler.TaskTriggerInfo, maxTriggersPerTask+1)
	for i := range tooMany {
		tooMany[i] = scheduler.TaskTriggerInfo{Type: "StartupTrigger"}
	}
	rawTooMany, _ := json.Marshal(tooMany)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ScheduledTasks/ExtractChapterImages/Triggers", bytes.NewReader(rawTooMany)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for >32 triggers, got %d", rec.Code)
	}

	// Valid triggers accepted
	validPayload := []scheduler.TaskTriggerInfo{
		{Type: "IntervalTrigger", IntervalTicks: &validInt},
		{Type: "DailyTrigger", TimeOfDayTicks: &validTOD},
		{Type: "WeeklyTrigger", TimeOfDayTicks: &validTOD, DayOfWeek: "Sunday"},
		{Type: "StartupTrigger"},
		{Type: "SystemEventTrigger"},
	}
	rawValid, _ := json.Marshal(validPayload)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ScheduledTasks/ExtractChapterImages/Triggers", bytes.NewReader(rawValid)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for valid triggers, got %d (%s)", rec.Code, rec.Body.String())
	}

	// 2. M-19: ExtractChapterImages & GenerateBifThumbnails localization (zh-CN, en-US, ja-JP, unknown fallback)
	for _, tc := range []struct {
		id       string
		locale   string
		wantName string
	}{
		{"ExtractChapterImages", "zh-CN", "提取章节缩略图"},
		{"ExtractChapterImages", "en-US", "Extract Chapter Images"},
		{"ExtractChapterImages", "ja-JP", "チャプター画像を抽出"},
		{"GenerateBifThumbnails", "zh-CN", "生成进度条缩略图 (BIF)"},
		{"GenerateBifThumbnails", "en-US", "Generate BIF Thumbnails"},
		{"GenerateBifThumbnails", "ko-KR", "BIF 썸네일 생성"},
	} {
		req := httptest.NewRequest(http.MethodGet, "/ScheduledTasks/"+tc.id, nil)
		req.Header.Set("Accept-Language", tc.locale)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s (%s): status=%d", tc.id, tc.locale, rec.Code)
		}
		var info scheduler.TaskInfo
		_ = json.Unmarshal(rec.Body.Bytes(), &info)
		if info.Name != tc.wantName || info.Description == "" {
			t.Fatalf("GET %s (%s): got Name=%q Desc=%q, want Name=%q", tc.id, tc.locale, info.Name, info.Description, tc.wantName)
		}
	}

	// Fallback to en-US when an unsupported locale is passed directly to localizeTaskInfo
	fb := localizeTaskInfo(scheduler.TaskInfo{Key: "GenerateBifThumbnails"}, "xx-XX")
	if fb.Name != "Generate BIF Thumbnails" {
		t.Fatalf("expected fallback to en-US, got %q", fb.Name)
	}
}
