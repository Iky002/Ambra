package dlna

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambra-media/ambra/internal/config"
)

func TestDLNAV0104DefaultProfileCaseInsensitiveDedup(t *testing.T) {
	_, mux, libRepo, store := setupDLNATestEnv(t)

	// 1. GET /Dlna/Profiles/DEFAULT resolves to canonical "default"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/Dlna/Profiles/DEFAULT", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /Dlna/Profiles/DEFAULT status=%d", rec.Code)
	}
	var p DlnaProfile
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if p.Id != defaultProfileID {
		t.Fatalf("expected canonical id %q, got %q", defaultProfileID, p.Id)
	}

	// 2. POST /Dlna/Profiles/Default is rejected with 400 (built-in default profile cannot be overwritten)
	updBody, _ := json.Marshal(DlnaProfile{Name: "Custom Default", FriendlyName: "Updated Default"})
	recUpd := httptest.NewRecorder()
	mux.ServeHTTP(recUpd, httptest.NewRequest(http.MethodPost, "/Dlna/Profiles/Default", bytes.NewReader(updBody)))
	if recUpd.Code != http.StatusBadRequest {
		t.Fatalf("POST /Dlna/Profiles/Default expected 400, got status=%d body=%s", recUpd.Code, recUpd.Body.String())
	}

	recList := httptest.NewRecorder()
	mux.ServeHTTP(recList, httptest.NewRequest(http.MethodGet, "/Dlna/ProfileInfos", nil))
	var list []DlnaProfile
	_ = json.Unmarshal(recList.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Id != defaultProfileID || list[0].Name != "Default" {
		t.Fatalf("expected single untouched default profile, got %+v", list)
	}

	// 3. DELETE /Dlna/Profiles/Default and /Dlna/Profiles/DEFAULT rejected with 400
	for _, variant := range []string{"Default", "DEFAULT", "default"} {
		recDel := httptest.NewRecorder()
		mux.ServeHTTP(recDel, httptest.NewRequest(http.MethodDelete, "/Dlna/Profiles/"+variant, nil))
		if recDel.Code != http.StatusBadRequest {
			t.Fatalf("DELETE /Dlna/Profiles/%s expected 400, got %d", variant, recDel.Code)
		}
	}

	// 4. Reload deduplicates legacy persisted "Default" + "default" entries
	legacyJSON, _ := json.Marshal([]DlnaProfile{
		{Id: "default", Name: "Old Default"},
		{Id: "Default", Name: "Legacy Duplicate Default"},
	})
	_, _ = store.Writer.Exec(`UPDATE server_config SET value_json = ? WHERE key = ?`, string(legacyJSON), profilesStoreKey)
	reloaded := New(config.Default(t.TempDir()), libRepo, store)
	reloaded.mu.RLock()
	count := len(reloaded.profiles)
	defP, ok := reloaded.profiles[defaultProfileID]
	reloaded.mu.RUnlock()
	if count != 1 || !ok || defP.Id != defaultProfileID {
		t.Fatalf("expected reloaded profiles deduplicated to 1 canonical default, got count=%d ok=%v def=%+v", count, ok, defP)
	}
}
