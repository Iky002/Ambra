package compatmedia

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/config"
	"github.com/ambra-media/ambra/internal/core/imaging"
	"github.com/ambra-media/ambra/internal/pkg/guid"
	"github.com/ambra-media/ambra/internal/plugins"
	"github.com/ambra-media/ambra/internal/store/sqlite"
)

func TestCompatMediaVirtualFoldersLibraryAccessControl(t *testing.T) {
	tmp := t.TempDir()
	store, err := sqlite.Open(filepath.Join(tmp, "cm_rbac.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	repo := sqlite.NewLibraryRepo(store)
	userRepo := sqlite.NewUserRepo(store)
	cfg := config.Default(tmp)

	libA, _ := repo.CreateLibrary(ctx, "AllowedLib", "movies", []string{filepath.Join(tmp, "a")})
	_, _ = repo.CreateLibrary(ctx, "SecretLib", "tvshows", []string{filepath.Join(tmp, "b")})

	restrictedPolicy := sqlite.DefaultUserPolicy(false)
	restrictedPolicy.EnableAllFolders = false
	restrictedPolicy.EnabledFolders = []string{libA.ID}
	restrictedUser := &sqlite.UserRecord{
		ID: guid.New(), Name: "restricted",
		Policy: restrictedPolicy, Config: sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, restrictedUser)

	adminUser := &sqlite.UserRecord{
		ID: guid.New(), Name: "admin", IsAdmin: true,
		Policy: sqlite.DefaultUserPolicy(true), Config: sqlite.DefaultUserConfig(),
	}
	_ = userRepo.CreateUser(ctx, adminUser)

	currentCaller := restrictedUser.ID
	h := New(repo, nil, imaging.New(repo), plugins.NewRegistry(), store, cfg)
	h.SetUserRepo(userRepo)
	router := h.BuildRouter(AuthFuncs{
		UserIDFromCtx: func(context.Context) string { return currentCaller },
	})

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	// Restricted user sees only libA
	rec := get("/Library/VirtualFolders/Query")
	if rec.Code != http.StatusOK {
		t.Fatalf("VirtualFolders/Query status=%d", rec.Code)
	}
	var vfRes dto.QueryResult[virtualFolderInfo]
	_ = json.Unmarshal(rec.Body.Bytes(), &vfRes)
	if vfRes.TotalRecordCount != 1 || len(vfRes.Items) != 1 || vfRes.Items[0].ItemId != libA.ID {
		t.Fatalf("restricted VirtualFolders/Query expected only libA, got %+v", vfRes)
	}

	rec = get("/Library/MediaFolders")
	var mfRes dto.QueryResult[dto.BaseItemDto]
	_ = json.Unmarshal(rec.Body.Bytes(), &mfRes)
	if mfRes.TotalRecordCount != 1 || len(mfRes.Items) != 1 || mfRes.Items[0].Id != libA.ID {
		t.Fatalf("restricted MediaFolders expected only libA, got %+v", mfRes)
	}

	rec = get("/Library/SelectableMediaFolders")
	var smfRes []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &smfRes)
	if len(smfRes) != 1 || smfRes[0]["Id"] != libA.ID {
		t.Fatalf("restricted SelectableMediaFolders expected only libA, got %+v", smfRes)
	}

	// Admin sees both libraries
	currentCaller = adminUser.ID
	rec = get("/Library/VirtualFolders/Query")
	vfRes = dto.QueryResult[virtualFolderInfo]{}
	_ = json.Unmarshal(rec.Body.Bytes(), &vfRes)
	if vfRes.TotalRecordCount != 2 {
		t.Fatalf("admin VirtualFolders/Query expected 2, got %d", vfRes.TotalRecordCount)
	}
}
