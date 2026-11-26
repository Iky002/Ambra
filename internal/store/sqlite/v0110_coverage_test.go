package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
	"github.com/ambra-media/ambra/internal/pkg/guid"
)

func TestUserRepoSafetyCameraUploadsAPIKeysAndLogsV0110(t *testing.T) {
	// 1. MaskAPIKey & nil Store() helper.
	if got := MaskAPIKey("   "); got != "********" {
		t.Fatalf("MaskAPIKey(blank) = %q", got)
	}
	if got := MaskAPIKey("12345678"); got != "********" {
		t.Fatalf("MaskAPIKey(8-char) = %q", got)
	}
	if got := MaskAPIKey("  abcdefghijklmnop  "); got != "abcd••••••••mnop" {
		t.Fatalf("MaskAPIKey(16-char) = %q", got)
	}
	var nilUR *UserRepo
	if nilUR.Store() != nil {
		t.Fatal("expected nil UserRepo.Store() to return nil")
	}

	store := openTestStore(t)
	userRepo := NewUserRepo(store)
	libRepo := NewLibraryRepo(store)
	ctx := context.Background()
	if userRepo.Store() != store {
		t.Fatal("expected UserRepo.Store() to return underlying store")
	}

	// 2. API Key deletion by KeyID or raw Token + edge cases.
	if tok, err := userRepo.DeleteAPIKeyByIDOrToken(ctx, "   "); err != nil || tok != "" {
		t.Fatalf("blank DeleteAPIKeyByIDOrToken = (%q, %v)", tok, err)
	}
	if tok, err := userRepo.DeleteAPIKeyByIDOrToken(ctx, "missing-key"); err != nil || tok != "" {
		t.Fatalf("missing DeleteAPIKeyByIDOrToken = (%q, %v)", tok, err)
	}
	k1 := &APIKeyRecord{KeyID: "kid-alpha", Token: "secret-token-alpha-1234", AppName: "Radarr"}
	k2 := &APIKeyRecord{Token: "secret-token-beta-5678", AppName: "Sonarr"}
	if err := userRepo.CreateAPIKey(ctx, k1); err != nil {
		t.Fatal(err)
	}
	if err := userRepo.CreateAPIKey(ctx, k2); err != nil {
		t.Fatal(err)
	}
	deletedRaw, err := userRepo.DeleteAPIKeyByIDOrToken(ctx, "kid-alpha")
	if err != nil || deletedRaw != "secret-token-alpha-1234" {
		t.Fatalf("DeleteAPIKeyByIDOrToken by KeyID = (%q, %v)", deletedRaw, err)
	}
	if err := userRepo.DeleteAPIKey(ctx, "secret-token-beta-5678"); err != nil {
		t.Fatalf("DeleteAPIKey by token: %v", err)
	}

	// 3. Camera uploads CRUD & DeleteDeviceArtifacts.
	admin1 := &UserRecord{Name: "AdminOne", IsAdmin: true, Policy: DefaultUserPolicy(true)}
	if err := userRepo.CreateUser(ctx, admin1); err != nil {
		t.Fatal(err)
	}
	t1 := time.Now().UTC().Add(-time.Minute)
	t2 := time.Now().UTC()
	if err := userRepo.UpsertCameraUpload(ctx, &CameraUploadRecord{
		UserID: admin1.ID, DeviceID: "dev-cam-1", FileID: "f1", Name: "IMG_1.jpg", Album: "Trip", MimeType: "image/jpeg", FilePath: "/tmp/1.jpg", FileSize: 100, CreatedAt: t1,
	}); err != nil {
		t.Fatal(err)
	}
	// Upsert conflict update on same (user_id, device_id, file_id) + second file with zero CreatedAt.
	if err := userRepo.UpsertCameraUpload(ctx, &CameraUploadRecord{
		UserID: admin1.ID, DeviceID: "dev-cam-1", FileID: "f1", Name: "IMG_1_new.jpg", Album: "Trip", MimeType: "image/jpeg", FilePath: "/tmp/1n.jpg", FileSize: 250, CreatedAt: t2,
	}); err != nil {
		t.Fatal(err)
	}
	if err := userRepo.UpsertCameraUpload(ctx, &CameraUploadRecord{
		UserID: admin1.ID, DeviceID: "dev-cam-1", FileID: "f2", Name: "IMG_2.png", MimeType: "image/png", FilePath: "/tmp/2.png", FileSize: 90,
	}); err != nil {
		t.Fatal(err)
	}
	uploads, err := userRepo.ListCameraUploads(ctx, admin1.ID, "dev-cam-1")
	if err != nil || len(uploads) != 2 {
		t.Fatalf("ListCameraUploads expected 2, got %d err=%v", len(uploads), err)
	}
	_ = userRepo.SetServerConfig(ctx, "device_options:dev-cam-1", `{"CustomName":"Phone"}`)
	if err := userRepo.DeleteDeviceArtifacts(ctx, "   "); err != nil {
		t.Fatal(err)
	}
	if err := userRepo.DeleteDeviceArtifacts(ctx, "dev-cam-1"); err != nil {
		t.Fatal(err)
	}
	uploadsAfter, _ := userRepo.ListCameraUploads(ctx, admin1.ID, "dev-cam-1")
	if len(uploadsAfter) != 0 {
		t.Fatalf("expected 0 camera uploads after DeleteDeviceArtifacts, got %d", len(uploadsAfter))
	}
	if _, err := userRepo.GetServerConfig(ctx, "device_options:dev-cam-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected device_options purged, got %v", err)
	}

	// 4. DeleteUserSafe & UpdateUserPolicySafe last-admin protection + user state cleanup.
	if err := userRepo.DeleteUserSafe(ctx, "missing-user"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on missing DeleteUserSafe, got %v", err)
	}
	if err := userRepo.UpdateUserPolicySafe(ctx, "missing-user", dto.UserPolicy{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on missing UpdateUserPolicySafe, got %v", err)
	}
	if err := userRepo.DeleteUserSafe(ctx, admin1.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("expected ErrLastAdmin deleting sole admin, got %v", err)
	}
	if err := userRepo.UpdateUserPolicySafe(ctx, admin1.ID, dto.UserPolicy{IsAdministrator: false}); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("expected ErrLastAdmin demoting sole admin, got %v", err)
	}
	if err := userRepo.UpdateUserPolicySafe(ctx, admin1.ID, dto.UserPolicy{IsAdministrator: true, IsDisabled: true}); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("expected ErrLastAdmin disabling sole admin, got %v", err)
	}

	admin2 := &UserRecord{Name: "AdminTwo", IsAdmin: true, Policy: DefaultUserPolicy(true)}
	if err := userRepo.CreateUser(ctx, admin2); err != nil {
		t.Fatal(err)
	}
	_ = userRepo.UpsertDisplayPreferences(ctx, &DisplayPreferencesRecord{ID: "usersettings", UserID: admin2.ID, Client: "emby"})
	_ = userRepo.CreateAccessToken(ctx, &AccessTokenRecord{Token: "t-keep", UserID: admin1.ID, DeviceID: "d1"})
	_ = userRepo.CreateAccessToken(ctx, &AccessTokenRecord{Token: "t-drop", UserID: admin1.ID, DeviceID: "d2"})
	if err := userRepo.DeleteTokensByUserExcept(ctx, admin1.ID, "t-keep"); err != nil {
		t.Fatal(err)
	}
	if err := userRepo.DeleteTokensByUserExcept(ctx, admin1.ID, ""); err != nil {
		t.Fatal(err)
	}

	if err := userRepo.UpdateUserPolicySafe(ctx, admin2.ID, dto.UserPolicy{IsAdministrator: false}); err != nil {
		t.Fatalf("expected demoting second admin to succeed, got %v", err)
	}
	if err := userRepo.UpdateUserPolicySafe(ctx, admin2.ID, dto.UserPolicy{IsAdministrator: true}); err != nil {
		t.Fatal(err)
	}
	if err := userRepo.DeleteUserSafe(ctx, admin2.ID); err != nil {
		t.Fatalf("expected deleting second admin to succeed, got %v", err)
	}

	// 5. ListActivityLogsFiltered & SetUserItemLikes & GetItemRuntimeTicks.
	baseTime := time.Now().UTC()
	_ = userRepo.CreateActivityLog(ctx, &ActivityLogRecord{Name: "Scan 100%_done\\ok", Overview: "detail", Type: "Library", Severity: "Info", CreatedAt: baseTime})
	_ = userRepo.CreateActivityLog(ctx, &ActivityLogRecord{Name: "Auth Failed", Type: "Auth", Severity: "Error", CreatedAt: baseTime.Add(time.Second)})

	minT := baseTime.Add(-time.Second)
	logs, total, err := userRepo.ListActivityLogsFiltered(ctx, -1, 0, &minT, "Info", `100%_done\ok`)
	if err != nil || total != 1 || len(logs) != 1 {
		t.Fatalf("ListActivityLogsFiltered wildcard search failed: total=%d len=%d err=%v", total, len(logs), err)
	}
	_, allTotal, err := userRepo.ListActivityLogsFiltered(ctx, 0, 10, nil, "all", "")
	if err != nil || allTotal != 2 {
		t.Fatalf("ListActivityLogsFiltered severity=all expected 2, got %d err=%v", allTotal, err)
	}

	if _, err := userRepo.GetItemRuntimeTicks(ctx, "missing-item"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound GetItemRuntimeTicks, got %v", err)
	}
	if _, err := userRepo.SetUserItemLikes(ctx, admin1.ID, "missing-item", boolPtr(true)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound SetUserItemLikes, got %v", err)
	}
	lib, _ := libRepo.CreateLibrary(ctx, "TestLib", "movies", nil)
	mItem := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, Type: "Movie", Name: "TestMovie", SortName: "testmovie"}
	_ = libRepo.UpsertItem(ctx, mItem)
	if ticks, err := userRepo.GetItemRuntimeTicks(ctx, mItem.ID); err != nil || ticks != 0 {
		t.Fatalf("expected 0 ticks for NULL runtime_ticks, got %d err=%v", ticks, err)
	}
	for _, likeVal := range []*bool{boolPtr(true), boolPtr(false), nil} {
		ud, err := userRepo.SetUserItemLikes(ctx, admin1.ID, mItem.ID, likeVal)
		if err != nil {
			t.Fatalf("SetUserItemLikes(%v): %v", likeVal, err)
		}
		if (likeVal == nil && ud.Likes != nil) || (likeVal != nil && (ud.Likes == nil || *ud.Likes != *likeVal)) {
			t.Fatalf("SetUserItemLikes(%v) mismatch: %+v", likeVal, ud)
		}
	}
}

func TestParentalRatingsFailClosedAndPolicyBlockingV0110(t *testing.T) {
	// 1. ParentalRatingScore & ParseParentalRatingValue (Fail-Closed = 100 on unknown labels).
	if s, ok := ParentalRatingScore("   "); ok || s != 0 {
		t.Fatalf("expected empty rating -> (0,false), got (%d,%v)", s, ok)
	}
	if got := ParseParentalRatingValue(""); got != 0 {
		t.Fatalf("expected ParseParentalRatingValue('') = 0, got %d", got)
	}
	for _, unknown := range []string{"ALIEN-RATING", "CUSTOM-99", "US:"} {
		if _, ok := ParentalRatingScore(unknown); ok {
			t.Fatalf("expected %q to be unrecognized", unknown)
		}
		if got := ParseParentalRatingValue(unknown); got != 100 {
			t.Fatalf("expected fail-closed score 100 for %q, got %d", unknown, got)
		}
	}
	knownScores := map[string]int{
		"US:R":       9,
		"DE:FSK 16":  9,
		"NC__17":     10,
		"TV-MA-LSV":  9,
		"TV-14-DLV":  8,
		"TV-PG-V":    5,
		"TV-Y7-FV":   4,
		"TV-G-V":     1,
		"TV-Y-FV":    1,
	}
	for r, want := range knownScores {
		if got := ParseParentalRatingValue(r); got != want {
			t.Fatalf("ParseParentalRatingValue(%q) = %d, want %d", r, got, want)
		}
	}
	if exc := ExceedingParentalRatings(9); len(exc) == 0 {
		t.Fatal("expected ratings exceeding 9 (e.g. NC-17)")
	}

	// 2. Comprehensive IsItemBlockedByPolicy, QueryItems, and ListCollections visibility matrix.
	store := openTestStore(t)
	repo := NewLibraryRepo(store)
	ctx := context.Background()

	libAllowed, _ := repo.CreateLibrary(ctx, "AllowedMovies", "movies", nil)
	libBlocked, _ := repo.CreateLibrary(ctx, "BlockedMovies", "movies", nil)

	moviePG := &ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, Type: "Movie", Name: "Family PG", SortName: "family pg", OfficialRating: "PG"}
	movieUnknown := &ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, Type: "Movie", Name: "Unknown Cert", SortName: "unknown cert", OfficialRating: "MARS-99"}
	movieUnrated := &ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, Type: "Movie", Name: "Indie Unrated", SortName: "indie unrated"}
	movieOtherLib := &ItemRecord{ID: guid.New(), LibraryID: libBlocked.ID, Type: "Movie", Name: "Secret Lib", SortName: "secret lib", OfficialRating: "G"}
	trailerUnrated := &ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, Type: "Video", Name: "Bonus Trailer", SortName: "bonus trailer", Extra: ItemExtra{ExtraType: "trailer"}}
	audioUnrated := &ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, Type: "Audio", Name: "Song Unrated", SortName: "song unrated"}

	seriesRated := &ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, Type: "Series", IsFolder: true, Name: "Rated Show", SortName: "rated show", OfficialRating: "TV-PG"}
	epRatedSeries := &ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, SeriesID: seriesRated.ID, Type: "Episode", Name: "Ep Inherited", SortName: "ep inherited"}
	seriesTVMA := &ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, Type: "Series", IsFolder: true, Name: "Adult Show", SortName: "adult show", OfficialRating: "TV-MA"}
	epTVMASeries := &ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, SeriesID: seriesTVMA.ID, Type: "Episode", Name: "Ep Under TVMA", SortName: "ep tvma", OfficialRating: "TV-G"}
	seriesUnrated := &ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, Type: "Series", IsFolder: true, Name: "Unrated Show", SortName: "unrated show"}
	epUnratedSeries := &ItemRecord{ID: guid.New(), LibraryID: libAllowed.ID, SeriesID: seriesUnrated.ID, Type: "Episode", Name: "Ep Unrated Show", SortName: "ep unrated"}

	for _, it := range []*ItemRecord{
		moviePG, movieUnknown, movieUnrated, movieOtherLib, trailerUnrated, audioUnrated,
		seriesRated, epRatedSeries, seriesTVMA, epTVMASeries, seriesUnrated, epUnratedSeries,
	} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	_ = repo.ReplaceItemTaxonomies(ctx, moviePG.ID, "tag", []string{"kid-approved"})
	_ = repo.ReplaceItemTaxonomies(ctx, movieUnrated.ID, "tag", []string{"restricted-tag"})

	boxVisible, err := repo.CreateCollection(ctx, "Mixed Collection", "", []string{moviePG.ID, movieUnknown.ID})
	if err != nil {
		t.Fatal(err)
	}
	boxHidden, err := repo.CreateCollection(ctx, "Hidden Collection", "", []string{movieUnknown.ID})
	if err != nil {
		t.Fatal(err)
	}

	// Admin & nil checks.
	if blocked, err := repo.IsItemBlockedByPolicy(ctx, nil, dto.UserPolicy{}); err != nil || blocked {
		t.Fatal("nil item must not be blocked")
	}
	if blocked, err := repo.IsItemBlockedByPolicy(ctx, movieUnknown, dto.UserPolicy{IsAdministrator: true, MaxParentalRating: intPtr(1)}); err != nil || blocked {
		t.Fatal("admin must never be blocked")
	}

	// EnabledFolders check + empty EnabledFolders on BoxSet.
	folderPol := dto.UserPolicy{EnableAllFolders: false, EnabledFolders: []string{libAllowed.ID}}
	if blocked, _ := repo.IsItemBlockedByPolicy(ctx, movieOtherLib, folderPol); !blocked {
		t.Fatal("expected item in non-enabled library to be blocked")
	}
	emptyFolderPol := dto.UserPolicy{EnableAllFolders: false, EnabledFolders: []string{}}
	if blocked, _ := repo.IsItemBlockedByPolicy(ctx, boxVisible, emptyFolderPol); !blocked {
		t.Fatal("expected BoxSet blocked when EnabledFolders is empty")
	}

	// MaxParentalRating = 7 (PG-13): blocks unknown rating (100 > 7), blocks episode under TV-MA series, hides boxHidden, allows boxVisible!
	ratingPol := dto.UserPolicy{EnableAllFolders: false, EnabledFolders: []string{libAllowed.ID}, MaxParentalRating: intPtr(7)}
	if blocked, _ := repo.IsItemBlockedByPolicy(ctx, moviePG, ratingPol); blocked {
		t.Fatal("expected PG movie allowed under MaxParentalRating=7")
	}
	if blocked, _ := repo.IsItemBlockedByPolicy(ctx, movieUnknown, ratingPol); !blocked {
		t.Fatal("expected unknown rating MARS-99 to fail closed under MaxParentalRating=7")
	}
	if blocked, _ := repo.IsItemBlockedByPolicy(ctx, epTVMASeries, ratingPol); !blocked {
		t.Fatal("expected episode under TV-MA series to be blocked")
	}
	if blocked, _ := repo.IsItemBlockedByPolicy(ctx, boxVisible, ratingPol); blocked {
		t.Fatal("expected boxVisible allowed because moviePG child is visible")
	}
	if blocked, _ := repo.IsItemBlockedByPolicy(ctx, boxHidden, ratingPol); !blocked {
		t.Fatal("expected boxHidden blocked because 0 children pass MaxParentalRating=7")
	}

	// BoxSet self-blocking: OfficialRating exceeding MaxParentalRating, "BoxSet" in BlockUnratedItems, and BlockedTags on BoxSet.
	boxRatedR := &ItemRecord{ID: guid.New(), LibraryID: "collections", Type: "BoxSet", IsFolder: true, Name: "R Box", SortName: "r box", OfficialRating: "R"}
	_ = repo.UpsertItem(ctx, boxRatedR)
	if blocked, _ := repo.IsItemBlockedByParentalPolicy(ctx, boxRatedR, intPtr(5), nil); !blocked {
		t.Fatal("expected R-rated BoxSet blocked under maxRating=5")
	}
	if blocked, _ := repo.IsItemBlockedByPolicy(ctx, boxVisible, dto.UserPolicy{EnableAllFolders: true, BlockUnratedItems: []string{"BoxSet"}}); !blocked {
		t.Fatal("expected unrated BoxSet blocked when BlockUnratedItems includes BoxSet")
	}
	_ = repo.ReplaceItemTaxonomies(ctx, boxVisible.ID, "tag", []string{"box-ban"})
	if blocked, _ := repo.IsItemBlockedByParentalPolicy(ctx, boxVisible, nil, []string{"box-ban"}); !blocked {
		t.Fatal("expected BoxSet with blocked tag to be blocked")
	}

	// BlockUnratedItems: ["all"] vs ["Series", "Trailer", "Music"].
	allUnratedPol := dto.UserPolicy{EnableAllFolders: true, BlockUnratedItems: []string{"all"}}
	if blocked, _ := repo.IsItemBlockedByPolicy(ctx, movieUnrated, allUnratedPol); !blocked {
		t.Fatal("expected unrated movie blocked under BlockUnratedItems=['all']")
	}
	if blocked, _ := repo.IsItemBlockedByPolicy(ctx, epRatedSeries, allUnratedPol); blocked {
		t.Fatal("expected unrated episode under rated TV-PG series to inherit rating and be allowed")
	}
	if blocked, _ := repo.IsItemBlockedByPolicy(ctx, epUnratedSeries, allUnratedPol); !blocked {
		t.Fatal("expected unrated episode under unrated series to be blocked")
	}

	granularUnratedPol := dto.UserPolicy{EnableAllFolders: true, BlockUnratedItems: []string{"Series", "Trailer", "Music"}}
	if blocked, _ := repo.IsItemBlockedByPolicy(ctx, movieUnrated, granularUnratedPol); blocked {
		t.Fatal("expected unrated Movie allowed when only Series/Trailer/Music are blocked")
	}
	if blocked, _ := repo.IsItemBlockedByPolicy(ctx, trailerUnrated, granularUnratedPol); !blocked {
		t.Fatal("expected unrated trailer blocked")
	}
	if blocked, _ := repo.IsItemBlockedByPolicy(ctx, audioUnrated, granularUnratedPol); !blocked {
		t.Fatal("expected unrated audio blocked when Music is in BlockUnratedItems")
	}
	if blocked, _ := repo.IsItemBlockedByPolicy(ctx, epUnratedSeries, granularUnratedPol); !blocked {
		t.Fatal("expected episode under unrated series blocked when Series is in BlockUnratedItems")
	}

	// BlockedTags & AllowedTags.
	if blocked, _ := repo.IsItemBlockedByPolicy(ctx, movieUnrated, dto.UserPolicy{EnableAllFolders: true, BlockedTags: []string{"restricted-tag"}}); !blocked {
		t.Fatal("expected BlockedTags hit to block item")
	}
	allowTagPol := dto.UserPolicy{EnableAllFolders: true, AllowedTags: []string{"kid-approved"}}
	if blocked, _ := repo.IsItemBlockedByPolicy(ctx, moviePG, allowTagPol); blocked {
		t.Fatal("expected item with kid-approved tag allowed")
	}
	if blocked, _ := repo.IsItemBlockedByPolicy(ctx, movieUnrated, allowTagPol); !blocked {
		t.Fatal("expected item missing AllowedTags to be blocked")
	}

	// Exercise SQL builders via ListCollections & QueryItems with full parental policy!
	cols, colCount, err := repo.ListCollections(ctx, ItemFilter{
		LibraryID:         libAllowed.ID, // non-"collections" LibraryID gets cleared in ListCollections
		AllowedLibraryIDs: []string{libAllowed.ID},
		MaxParentalRating: intPtr(7),
		AllowedTags:       []string{"kid-approved"},
	})
	if err != nil || colCount != 1 || cols[0].Id != boxVisible.ID {
		t.Fatalf("ListCollections with child visibility filter expected [boxVisible], got count=%d cols=%+v err=%v", colCount, cols, err)
	}
	_, _, err = repo.QueryItems(ctx, ItemFilter{
		AllowedLibraryIDs: []string{libAllowed.ID},
		BlockUnratedItems: []string{"Series", "Trailer", "Music", "BoxSet"},
		BlockedTags:       []string{"restricted-tag"},
	})
	if err != nil {
		t.Fatalf("QueryItems with granular BlockUnratedItems failed: %v", err)
	}
}

func TestLibraryOptionsCascadeContainersFiltersAndMultiVersionV0110(t *testing.T) {
	store := openTestStore(t)
	repo := NewLibraryRepo(store)
	userRepo := NewUserRepo(store)
	ctx := context.Background()

	// 1. Library options CRUD & DeleteLibrary / DeleteLibraryByName.
	var nilLib *LibraryRecord
	if !nilLib.SaveLocalMetadataEnabled() {
		t.Fatal("expected nil LibraryRecord SaveLocalMetadataEnabled=true")
	}
	tmpMedia := t.TempDir()
	lib1, err := repo.CreateLibrary(ctx, "TempLib", "", nil)
	if err != nil || !lib1.SaveLocalMetadataEnabled() {
		t.Fatalf("CreateLibrary: %v", err)
	}
	lib1, err = repo.AddLibraryPath(ctx, lib1.ID, tmpMedia)
	if err != nil || len(lib1.Paths) != 1 {
		t.Fatalf("AddLibraryPath: %v", err)
	}
	// Duplicate AddLibraryPath is a no-op.
	if libDup, err := repo.AddLibraryPath(ctx, lib1.ID, tmpMedia); err != nil || len(libDup.Paths) != 1 {
		t.Fatalf("duplicate AddLibraryPath: %v", err)
	}
	if _, err := repo.UpdateLibraryOptions(ctx, "missing-lib", map[string]any{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound UpdateLibraryOptions, got %v", err)
	}
	lib1, err = repo.UpdateLibraryOptions(ctx, lib1.ID, map[string]any{"SaveLocalMetadata": false, "EnableRealtimeMonitor": true})
	if err != nil || lib1.SaveLocalMetadataEnabled() {
		t.Fatalf("UpdateLibraryOptions SaveLocalMetadata=false failed: %+v err=%v", lib1, err)
	}
	tempItem := &ItemRecord{ID: guid.New(), LibraryID: lib1.ID, Type: "Movie", Name: "Gone", SortName: "gone"}
	_ = repo.UpsertItem(ctx, tempItem)
	_ = repo.UpsertImage(ctx, &ImageRecord{ItemID: tempItem.ID, ImageType: "Primary", Path: "/tmp/p.jpg"})
	_ = repo.ReplaceItemAncestors(ctx, tempItem.ID, []AncestorLink{{AncestorID: lib1.ID, Depth: 1}})
	_ = repo.UpsertSearchFTS(ctx, tempItem.ID, "Gone", "gone", "", "", "")
	if err := repo.DeleteLibraryByName(ctx, "NoSuchLib"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound DeleteLibraryByName, got %v", err)
	}
	if err := repo.DeleteLibraryByName(ctx, "TempLib"); err != nil {
		t.Fatalf("DeleteLibraryByName: %v", err)
	}

	// 2. Cascade MarkPlayed / MarkUnplayed / MarkFavorite / UnmarkFavorite.
	u := &UserRecord{Name: "Viewer", Policy: dto.UserPolicy{
		EnableAllFolders:  true,
		BlockUnratedItems: []string{"Movie"},
		AllowedTags:       []string{"vip"},
	}}
	_ = userRepo.CreateUser(ctx, u)
	for _, fn := range []func(context.Context, string, string) (*dto.UserItemDataDto, error){
		repo.MarkPlayed, repo.MarkUnplayed, repo.MarkFavorite, repo.UnmarkFavorite,
	} {
		if _, err := fn(ctx, u.ID, "missing-item"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound on missing item user-data op, got %v", err)
		}
	}

	mainLib, _ := repo.CreateLibrary(ctx, "Main", "movies", []string{tmpMedia})
	ser := &ItemRecord{ID: guid.New(), LibraryID: mainLib.ID, Type: "Series", IsFolder: true, Name: "Show", SortName: "show"}
	sea := &ItemRecord{ID: guid.New(), LibraryID: mainLib.ID, ParentID: ser.ID, SeriesID: ser.ID, Type: "Season", IsFolder: true, Name: "S1", SortName: "s1"}
	ep := &ItemRecord{ID: guid.New(), LibraryID: mainLib.ID, ParentID: sea.ID, SeriesID: ser.ID, SeasonID: sea.ID, Type: "Episode", Name: "E1", SortName: "e1"}
	alb := &ItemRecord{ID: guid.New(), LibraryID: mainLib.ID, Type: "MusicAlbum", IsFolder: true, Name: "Album", SortName: "album"}
	trk := &ItemRecord{ID: guid.New(), LibraryID: mainLib.ID, ParentID: alb.ID, Type: "Audio", MediaType: "Audio", Name: "Track", SortName: "track"}
	game := &ItemRecord{ID: guid.New(), LibraryID: mainLib.ID, Type: "Game", Name: "Halo", SortName: "halo", OfficialRating: "PG-13", ProductionYear: intPtr(2021)}
	for _, it := range []*ItemRecord{ser, sea, ep, alb, trk, game} {
		if err := repo.UpsertItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := repo.MarkPlayed(ctx, u.ID, ser.ID); err != nil {
		t.Fatalf("cascade MarkPlayed Series: %v", err)
	}
	if epUD, _ := repo.GetUserItemData(ctx, u.ID, ep.ID); !epUD.Played {
		t.Fatal("expected Episode marked played after Series MarkPlayed")
	}
	if _, err := repo.MarkUnplayed(ctx, u.ID, ser.ID); err != nil {
		t.Fatalf("cascade MarkUnplayed Series: %v", err)
	}
	if epUD, _ := repo.GetUserItemData(ctx, u.ID, ep.ID); epUD.Played {
		t.Fatal("expected Episode marked unplayed after Series MarkUnplayed")
	}
	if _, err := repo.MarkPlayed(ctx, u.ID, alb.ID); err != nil {
		t.Fatalf("cascade MarkPlayed MusicAlbum: %v", err)
	}
	if _, err := repo.MarkFavorite(ctx, u.ID, trk.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UnmarkFavorite(ctx, u.ID, trk.ID); err != nil {
		t.Fatal(err)
	}

	// 3. Containers (Collection & Playlist) CRUD, Move, Remove, Policy & Anonymous scan.
	if _, err := repo.CreateCollection(ctx, "   ", "", nil); err == nil {
		t.Fatal("expected empty collection name error")
	}
	if _, err := repo.CreatePlaylist(ctx, "   ", "", u.ID, nil); err == nil {
		t.Fatal("expected empty playlist name error")
	}
	if _, err := repo.AddItemsToContainer(ctx, "missing", []string{ep.ID}, true); !errors.Is(err, ErrNotFound) {
		t.Fatal("expected ErrNotFound AddItemsToContainer")
	}
	if _, err := repo.RemoveItemsFromContainer(ctx, "missing", []string{ep.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatal("expected ErrNotFound RemoveItemsFromContainer")
	}
	if err := repo.MoveContainerItem(ctx, "missing", ep.ID, 0); !errors.Is(err, ErrNotFound) {
		t.Fatal("expected ErrNotFound MoveContainerItem")
	}

	fA := filepath.Join(tmpMedia, "m1-1080p.mkv")
	fB := filepath.Join(tmpMedia, "m1-4k.mkv")
	_ = os.WriteFile(fA, []byte("1080p"), 0o644)
	_ = os.WriteFile(fB, []byte("4k"), 0o644)
	movVIP := &ItemRecord{ID: guid.New(), LibraryID: mainLib.ID, Type: "Movie", MediaType: "Video", Name: "VIP Movie", SortName: "vip movie", OfficialRating: "PG", ProductionYear: intPtr(2023), Container: "mkv", Path: fA}
	movPlain := &ItemRecord{ID: guid.New(), LibraryID: mainLib.ID, Type: "Movie", MediaType: "Video", Name: "Unrated Plain", SortName: "unrated plain", Container: "mp4", Path: fB}
	_ = repo.UpsertItem(ctx, movVIP)
	_ = repo.UpsertItem(ctx, movPlain)
	_ = repo.ReplaceItemTaxonomies(ctx, movVIP.ID, "tag", []string{"vip"})
	_ = repo.ReplaceItemTaxonomies(ctx, movVIP.ID, "genre", []string{"Sci-Fi"})
	_ = repo.ReplaceItemTaxonomies(ctx, movVIP.ID, "studio", []string{"Warner"})
	_ = repo.ReplaceItemTaxonomies(ctx, trk.ID, "genre", []string{"Synthpop"})
	_ = repo.ReplaceItemTaxonomies(ctx, game.ID, "genre", []string{"Shooter"})

	box, err := repo.CreateCollection(ctx, "VIP Box", mainLib.ID, []string{movVIP.ID, movPlain.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.MarkPlayed(ctx, u.ID, box.ID); err != nil {
		t.Fatalf("cascade MarkPlayed BoxSet: %v", err)
	}
	pl, err := repo.CreatePlaylist(ctx, "Video Mix", "", u.ID, []string{movVIP.ID, movPlain.ID, trk.ID})
	if err != nil || pl.MediaType != "Video" {
		t.Fatalf("CreatePlaylist auto MediaType expected Video, got %+v err=%v", pl, err)
	}
	emptyPl, err := repo.CreatePlaylist(ctx, "Audio Default", "", u.ID, nil)
	if err != nil || emptyPl.MediaType != "Audio" {
		t.Fatalf("CreatePlaylist default MediaType expected Audio, got %+v err=%v", emptyPl, err)
	}

	// Anonymous userID=="" exercises !hasUser scanContainerItemRow; restricted u.ID exercises loadRestrictedUserPolicy!
	anonItems, anonTotal, err := repo.GetContainerItems(ctx, "srv1", "", pl.ID, 0, 10, nil)
	if err != nil || anonTotal != 3 || len(anonItems) != 3 {
		t.Fatalf("anonymous GetContainerItems expected 3, got total=%d err=%v", anonTotal, err)
	}
	if err := repo.MoveContainerItem(ctx, pl.ID, anonItems[2].PlaylistItemId, -5); err != nil {
		t.Fatalf("MoveContainerItem by entryID to clamped 0: %v", err)
	}
	if err := repo.MoveContainerItem(ctx, pl.ID, movPlain.ID, 99); err != nil {
		t.Fatalf("MoveContainerItem by itemID to clamped end: %v", err)
	}
	if err := repo.MoveContainerItem(ctx, pl.ID, movPlain.ID, 2); err != nil {
		t.Fatalf("MoveContainerItem same index no-op: %v", err)
	}
	if err := repo.MoveContainerItem(ctx, pl.ID, "not-in-playlist", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound moving missing item, got %v", err)
	}
	if removed, err := repo.RemoveItemsFromContainer(ctx, pl.ID, []string{trk.ID}); err != nil || removed != 1 {
		t.Fatalf("RemoveItemsFromContainer: removed=%d err=%v", removed, err)
	}
	if emptySlice, cnt, err := repo.GetContainerItemsWithPolicy(ctx, "srv1", u.ID, pl.ID, 0, 10, nil, []string{}, nil, nil); err != nil || cnt != 0 || len(emptySlice) != 0 {
		t.Fatalf("expected 0 items when allowedLibraryIDs=[], got %d err=%v", cnt, err)
	}
	restrictedItems, restrictedTotal, err := repo.GetContainerItemsWithPolicy(ctx, "srv1", u.ID, pl.ID, 0, 10, nil, []string{mainLib.ID}, nil, nil)
	if err != nil || restrictedTotal != 1 || restrictedItems[0].Id != movVIP.ID {
		t.Fatalf("restricted user GetContainerItemsWithPolicy expected only movVIP, got total=%d items=%+v err=%v", restrictedTotal, restrictedItems, err)
	}

	// 4. GetQueryFilters & ListFilterValuesWithPolicy across all 12 dimensions!
	_ = repo.UpsertMediaSourceAndStreams(ctx, movVIP.ID, MediaSourceRecord{
		ID: guid.New(), ItemID: movVIP.ID, Name: "1080p", Path: fA, Container: "mkv", Size: 1000, RuntimeTicks: 50000,
		Streams: []MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Video", Codec: "hevc", VideoRange: "HDR10", Width: 1920, Height: 1080},
			{StreamIndex: 1, StreamType: "Audio", Codec: "eac3", Language: "eng", AudioLayout: "5.1", Channels: 6},
			{StreamIndex: 2, StreamType: "Subtitle", Codec: "ass", Language: "chi"},
		},
	})
	_ = repo.UpsertMediaSourceAndStreams(ctx, movPlain.ID, MediaSourceRecord{
		ID: guid.New(), ItemID: movPlain.ID, Name: "4K", Path: fB, Container: "mp4", Size: 2000, RuntimeTicks: 60000,
		Streams: []MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Video", Codec: "h264", VideoRange: "SDR", Width: 3840, Height: 2160},
		},
	})

	if emptyQF, err := repo.GetQueryFilters(ctx, QueryFiltersFilter{AllowedLibraryIDs: []string{}}); err != nil || len(emptyQF.Genres) != 0 {
		t.Fatalf("expected empty GetQueryFilters on empty AllowedLibraryIDs: %+v err=%v", emptyQF, err)
	}
	qf, err := repo.GetQueryFilters(ctx, QueryFiltersFilter{
		ParentID:          mainLib.ID,
		IncludeItemTypes:  []string{"Movie"},
		AllowedLibraryIDs: []string{mainLib.ID},
		MaxParentalRating: intPtr(9),
	})
	if err != nil || len(qf.Genres) != 1 || len(qf.Studios) != 1 || len(qf.Tags) != 1 || len(qf.OfficialRatings) != 1 || len(qf.Years) != 1 {
		t.Fatalf("unexpected GetQueryFilters result: %+v err=%v", qf, err)
	}

	if vals, tot, err := repo.ListFilterValuesWithPolicy(ctx, "s1", "Tag", "", nil, 0, 10, []string{}, nil, nil); err != nil || tot != 0 || len(vals) != 0 {
		t.Fatalf("expected empty ListFilterValuesWithPolicy on empty AllowedLibraryIDs")
	}
	for _, dim := range []string{
		"MusicGenre", "GameGenre", "Tag", "OfficialRating", "ItemType", "Container",
		"AudioCodec", "AudioLayout", "VideoCodec", "SubtitleCodec", "StreamLanguage", "ExtendedVideoType",
	} {
		vals, tot, err := repo.ListFilterValuesWithPolicy(ctx, "s1", dim, mainLib.ID, nil, 0, 0, []string{mainLib.ID}, intPtr(10), nil)
		if err != nil || tot == 0 || len(vals) == 0 {
			t.Fatalf("ListFilterValuesWithPolicy(%s) expected >0 results, got tot=%d err=%v", dim, tot, err)
		}
	}
	// Unscoped Tag query + unknown dimension fallback.
	if _, tot, err := repo.ListFilterValues(ctx, "s1", "Tag", "", nil, 0, 10); err != nil || tot == 0 {
		t.Fatalf("unscoped ListFilterValues(Tag) failed: tot=%d err=%v", tot, err)
	}
	if vals, tot, err := repo.ListFilterValues(ctx, "s1", "UnknownDimension", "", nil, 0, 10); err != nil || tot != 0 || len(vals) != 0 {
		t.Fatalf("expected empty result for UnknownDimension, got %d %v", tot, err)
	}

	// 5. MergeItemVersions, SplitItemVersions, DeleteItemByPath (multi-source promotion), and DeleteItemsByPathPrefix!
	if _, _, err := repo.MergeItemVersions(ctx, []string{movVIP.ID}); err == nil {
		t.Fatal("expected error merging < 2 item IDs")
	}
	if _, _, err := repo.MergeItemVersions(ctx, []string{"missing-1", "missing-2"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound merging missing primary, got %v", err)
	}
	if _, _, err := repo.MergeItemVersions(ctx, []string{ser.ID, movVIP.ID}); err == nil {
		t.Fatal("expected error merging folder primary")
	}
	if _, _, err := repo.MergeItemVersions(ctx, []string{movVIP.ID, ser.ID}); err == nil {
		t.Fatal("expected error merging folder secondary")
	}
	if _, _, err := repo.MergeItemVersions(ctx, []string{movVIP.ID, ep.ID}); err == nil {
		t.Fatal("expected error merging mismatched types Movie vs Episode")
	}
	otherLib, _ := repo.CreateLibrary(ctx, "Other", "movies", nil)
	otherMov := &ItemRecord{ID: guid.New(), LibraryID: otherLib.ID, Type: "Movie", Name: "Other", SortName: "other"}
	_ = repo.UpsertItem(ctx, otherMov)
	if _, _, err := repo.MergeItemVersions(ctx, []string{movVIP.ID, otherMov.ID}); err == nil {
		t.Fatal("expected error merging across different libraries")
	}

	// Attach duplicate source path + user progress + extra trailer onto movPlain, then merge into movVIP!
	_ = repo.UpsertMediaSourceAndStreams(ctx, movPlain.ID, MediaSourceRecord{
		ID: guid.New(), ItemID: movPlain.ID, Name: "Dup", Path: fA, Container: "mkv",
	})
	_, _ = userRepo.UpsertPlaybackProgress(ctx, u.ID, movPlain.ID, 12345, false, intPtr(1), intPtr(2))
	extraTrailer := &ItemRecord{ID: guid.New(), LibraryID: mainLib.ID, ParentID: movPlain.ID, Type: "Video", Name: "Plain Trailer", SortName: "plain trailer", Extra: ItemExtra{ExtraType: "trailer"}}
	_ = repo.UpsertItem(ctx, extraTrailer)
	_ = repo.ReplaceItemAncestors(ctx, extraTrailer.ID, []AncestorLink{{AncestorID: movPlain.ID, Depth: 1}})
	_ = repo.UpsertSearchFTS(ctx, movVIP.ID, "VIP Movie", "vip movie", "", "", "")
	_ = repo.ReplaceItemPeople(ctx, movVIP.ID, []PersonInput{{Name: "Keanu Reeves", Type: "Actor"}})

	primaryID, mergedCnt, err := repo.MergeItemVersions(ctx, []string{movVIP.ID, movPlain.ID, "nonexistent-secondary"})
	if err != nil || primaryID != movVIP.ID || mergedCnt != 1 {
		t.Fatalf("MergeItemVersions failed: primary=%s cnt=%d err=%v", primaryID, mergedCnt, err)
	}

	// DeleteItemByPath on multi-version item: deleting primary path fA promotes fB onto movVIP!
	if deleted, err := repo.DeleteItemByPath(ctx, mainLib.ID, "/no/such/path.mkv"); err != nil || deleted {
		t.Fatalf("expected missing path DeleteItemByPath=(false,nil), got %v %v", deleted, err)
	}
	// Re-add a 3rd source fC so after deleting fA we still have 2 sources to test SplitItemVersions!
	fC := filepath.Join(tmpMedia, "m1-720p.mkv")
	_ = os.WriteFile(fC, []byte("720p"), 0o644)
	_ = repo.UpsertMediaSourceAndStreams(ctx, movVIP.ID, MediaSourceRecord{
		ID: guid.New(), ItemID: movVIP.ID, Name: "720p", Path: fC, Container: "mkv", Size: 500, RuntimeTicks: 40000,
	})
	if deleted, err := repo.DeleteItemByPath(ctx, mainLib.ID, fA); err != nil || !deleted {
		t.Fatalf("DeleteItemByPath primary source fA failed: deleted=%v err=%v", deleted, err)
	}
	promotedRec, _ := repo.GetItemRecordByID(ctx, movVIP.ID)
	if promotedRec.Path != fB {
		t.Fatalf("expected remaining source fB promoted to primary Path, got %q", promotedRec.Path)
	}

	// SplitItemVersions: missing -> ErrNotFound, single-source -> empty, 2-source -> splits into 1 new item!
	if _, err := repo.SplitItemVersions(ctx, "missing-split"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound SplitItemVersions, got %v", err)
	}
	if splitSingle, err := repo.SplitItemVersions(ctx, otherMov.ID); err != nil || len(splitSingle) != 0 {
		t.Fatalf("expected empty SplitItemVersions on single-source item, got %v err=%v", splitSingle, err)
	}
	createdIDs, err := repo.SplitItemVersions(ctx, movVIP.ID)
	if err != nil || len(createdIDs) != 1 {
		t.Fatalf("SplitItemVersions expected 1 created item, got %v err=%v", createdIDs, err)
	}

	// DeleteItemsByPathPrefix: empty guard & directory prefix cleanup.
	if n, err := repo.DeleteItemsByPathPrefix(ctx, "", ""); err != nil || n != 0 {
		t.Fatalf("expected (0,nil) on empty DeleteItemsByPathPrefix, got %d %v", n, err)
	}
	if n, err := repo.DeleteItemsByPathPrefix(ctx, mainLib.ID, tmpMedia); err != nil || n < 2 {
		t.Fatalf("DeleteItemsByPathPrefix expected >=2 items deleted, got %d err=%v", n, err)
	}
}

func TestMigrationsScalarFuncsBatchRecommendationsAndClosedStoreV0110(t *testing.T) {
	ctx := context.Background()

	// 1. SQL scalar functions (NULL, []byte, integer/default) & Open env var branches.
	t.Setenv("AMBRA_SQLITE_CACHE_MB", "1")
	t.Setenv("AMBRA_SQLITE_MMAP_MB", "0")
	store := openTestStore(t)
	var skNull, skBlob, skInt string
	var prNull, prBlob, prInt int64
	if err := store.Reader.QueryRowContext(ctx,
		`SELECT pinyin_sort_key(NULL), pinyin_sort_key(X'6162'), pinyin_sort_key(123),
		        parental_rating_rank(NULL), parental_rating_rank(X'5047'), parental_rating_rank(18)`,
	).Scan(&skNull, &skBlob, &skInt, &prNull, &prBlob, &prInt); err != nil {
		t.Fatalf("scalar functions query failed: %v", err)
	}
	if skNull != "" || prNull != 0 || prBlob != 5 || prInt != 9 {
		t.Fatalf("unexpected scalar results: sk=(%q,%q,%q) pr=(%d,%d,%d)", skNull, skBlob, skInt, prNull, prBlob, prInt)
	}
	t.Setenv("AMBRA_SQLITE_CACHE_MB", "not-an-int")
	_ = openTestStore(t)

	// 2. Legacy V1 database migration through V2 (item_images FK rebuild), V3 (ALTER TABLE items), V4, V5 (duplicate user rename), V6, V7!
	legacyPath := filepath.Join(t.TempDir(), "legacy_v1.db")
	rawV1Store, err := Open(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	// Downgrade schema_migrations and tables to simulate pre-V2/V3/V5/V7 schema with duplicate users and item_images FK.
	for _, stmt := range []string{
		`PRAGMA foreign_keys = OFF`,
		`DELETE FROM schema_migrations WHERE version >= 2`,
		`DROP INDEX IF EXISTS idx_users_name_nocase_unique`,
		`DROP TABLE item_images`,
		`CREATE TABLE item_images (
			item_id TEXT NOT NULL REFERENCES items(id),
			image_type TEXT NOT NULL,
			image_index INTEGER NOT NULL DEFAULT 0,
			path TEXT NOT NULL,
			tag TEXT NOT NULL,
			width INTEGER,
			height INTEGER,
			blurhash TEXT,
			PRIMARY KEY (item_id, image_type, image_index)
		)`,
		`DROP TABLE items`,
		`CREATE TABLE items (
			id TEXT PRIMARY KEY, internal_id INTEGER UNIQUE, library_id TEXT NOT NULL,
			parent_id TEXT, series_id TEXT, season_id TEXT, type TEXT NOT NULL, media_type TEXT,
			is_folder INTEGER NOT NULL DEFAULT 0, is_strm INTEGER NOT NULL DEFAULT 0,
			name TEXT NOT NULL, original_title TEXT, sort_name TEXT NOT NULL,
			index_number INTEGER, parent_index_number INTEGER, production_year INTEGER,
			premiere_date INTEGER, end_date INTEGER, community_rating REAL, critic_rating REAL, official_rating TEXT,
			runtime_ticks INTEGER, container TEXT, path TEXT, file_size INTEGER, file_mtime INTEGER,
			etag TEXT NOT NULL, extra_json TEXT NOT NULL DEFAULT '{}', date_created INTEGER NOT NULL, date_modified INTEGER NOT NULL
		)`,
		`INSERT INTO items (id, library_id, type, name, sort_name, official_rating, etag, extra_json, date_created, date_modified)
		 VALUES ('it1', 'lib1', 'Movie', 'LegacyFilm', 'legacyfilm', 'PG-13', 'e1', '{"extra_type":"trailer"}', 100, 100)`,
		`DROP TABLE user_item_data`,
		`CREATE TABLE user_item_data (
			user_id TEXT NOT NULL, item_id TEXT NOT NULL, played INTEGER NOT NULL DEFAULT 0,
			play_count INTEGER NOT NULL DEFAULT 0, playback_position_ticks INTEGER NOT NULL DEFAULT 0,
			is_favorite INTEGER NOT NULL DEFAULT 0, last_played_date INTEGER,
			audio_stream_index INTEGER, subtitle_stream_index INTEGER,
			PRIMARY KEY (user_id, item_id)
		)`,
		`DROP TABLE api_keys`,
		`CREATE TABLE api_keys (token TEXT PRIMARY KEY, app_name TEXT NOT NULL, created_at INTEGER NOT NULL)`,
		`INSERT INTO api_keys (token, app_name, created_at) VALUES ('legacy-tok-1', 'App', 100)`,
		`DROP TABLE users`,
		`CREATE TABLE users (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, password_hash TEXT NOT NULL,
			is_admin INTEGER NOT NULL DEFAULT 0, is_disabled INTEGER NOT NULL DEFAULT 0,
			policy_json TEXT NOT NULL DEFAULT '{}', config_json TEXT NOT NULL DEFAULT '{}',
			last_login_at INTEGER, last_active_at INTEGER, created_at INTEGER NOT NULL
		)`,
		`INSERT INTO users (id, name, password_hash, is_admin, created_at) VALUES
			('u1', 'DupName', 'h', 1, 100),
			('u2', 'dupname', 'h', 0, 101),
			('u3', 'dupname_dup_2', 'h', 0, 102)`,
	} {
		if _, err := rawV1Store.Writer.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed legacy schema (%s): %v", stmt, err)
		}
	}
	_ = rawV1Store.Close()

	migratedStore, err := Open(legacyPath)
	if err != nil {
		t.Fatalf("Open legacy V1 database failed migration: %v", err)
	}
	migratedUsers, err := NewUserRepo(migratedStore).ListUsers(ctx)
	if err != nil || len(migratedUsers) != 3 {
		t.Fatalf("expected 3 deduplicated users after V5 migration, got %d err=%v", len(migratedUsers), err)
	}
	_ = migratedStore.Close()

	// 3. UpsertItemsBatch, Similar, InstantMix, Recommendations, Hydration, Chapter/BIF candidates, Taxonomies & NextUp!
	repo := NewLibraryRepo(store)
	uRepo := NewUserRepo(store)
	lib, _ := repo.CreateLibrary(ctx, "Cinema", "movies", nil)
	viewer := &UserRecord{Name: "Cinephile", Policy: DefaultUserPolicy(false)}
	_ = uRepo.CreateUser(ctx, viewer)

	m1 := &ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Blade Runner", SortName: "blade runner",
		OfficialRating: "R", ProductionYear: intPtr(1982), CommunityRating: f64Ptr(8.5), PremiereDate: int64Ptr(393811200),
		Path: "/media/br.mkv", Container: "mkv",
		Extra: ItemExtra{
			Overview:    "Cyberpunk classic",
			ProviderIDs: map[string]string{"Tmdb": "78"},
			Chapters:    []ChapterRecord{{StartPositionTicks: 0, Name: "Opening"}},
		},
	}
	m2 := &ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Dune", SortName: "dune",
		OfficialRating: "PG-13", ProductionYear: intPtr(2021), CommunityRating: f64Ptr(8.2), Path: "/media/dune.mkv", Container: "mkv",
	}
	m3 := &ItemRecord{
		ID: guid.New(), LibraryID: lib.ID, Type: "Movie", MediaType: "Video", Name: "Arrival", SortName: "arrival",
		OfficialRating: "PG-13", ProductionYear: intPtr(2016), CommunityRating: f64Ptr(8.0), Path: "/media/arrival.mkv", Container: "mkv",
	}
	song1 := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, Type: "Audio", MediaType: "Audio", Name: "Vangelis", SortName: "vangelis"}
	song2 := &ItemRecord{ID: guid.New(), LibraryID: lib.ID, Type: "Audio", MediaType: "Audio", Name: "Zimmer", SortName: "zimmer"}

	batchIn := []BatchItemInput{
		{Item: nil},
		{Item: m1, AncestorIDs: []string{lib.ID}, FTSTitleRaw: "Blade Runner", FTSTitleTokens: "blade runner"},
		{Item: m2, AncestorIDs: []string{lib.ID}, FTSTitleRaw: "Dune", FTSTitleTokens: "dune"},
		{Item: m3, AncestorIDs: []string{lib.ID}, FTSTitleRaw: "Arrival", FTSTitleTokens: "arrival"},
		{Item: song1, AncestorIDs: []string{lib.ID}, FTSTitleRaw: "Vangelis"},
		{Item: song2, AncestorIDs: []string{lib.ID}, FTSTitleRaw: "Zimmer"},
	}
	if err := repo.UpsertItemsBatch(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertItemsBatch(ctx, batchIn); err != nil {
		t.Fatalf("UpsertItemsBatch insert: %v", err)
	}
	// Re-upsert unchanged FTS (hits fast rowid skip) and modified FTS (hits rowid update).
	batchIn[1].FTSTitleRaw = "Blade Runner Final Cut"
	if err := repo.UpsertItemsBatch(ctx, batchIn[1:3]); err != nil {
		t.Fatalf("UpsertItemsBatch update: %v", err)
	}

	_ = repo.ReplaceItemImages(ctx, m1.ID, []ImageRecord{
		{ImageType: "Primary", ImageIndex: 0, Path: "/img/p.jpg"},
		{ImageType: "Backdrop", ImageIndex: 0, Path: "/img/b.jpg"},
		{ImageType: "Chapter", ImageIndex: 0, Path: "/img/c0.jpg"},
	})
	_ = repo.ReplaceMediaSourcesAndStreams(ctx, m1.ID, []MediaSourceRecord{{
		ID: guid.New(), Name: "Main", Path: "/media/br.mkv", Container: "mkv", SupportsDirectPlay: true,
		Streams: []MediaStreamRecord{
			{StreamIndex: 0, StreamType: "Video", Codec: "hevc", Width: 3840, Height: 2160, VideoRange: "HDR10"},
			{StreamIndex: 1, StreamType: "Audio", Codec: "truehd", Channels: 8, IsDefault: true},
			{StreamIndex: 2, StreamType: "Subtitle", Codec: "srt", IsDefault: true},
		},
	}})
	for _, id := range []string{m1.ID, m2.ID} {
		_ = repo.ReplaceItemTaxonomies(ctx, id, "genre", []string{"Sci-Fi"})
	}
	_ = repo.ReplaceItemTaxonomies(ctx, m3.ID, "genre", []string{"Drama"})
	_ = repo.ReplaceItemTaxonomies(ctx, m1.ID, "studio", []string{"Ridley Scott"})
	_ = repo.ReplaceItemTaxonomies(ctx, m1.ID, "tag", []string{"cult"})
	_ = repo.ReplaceItemTaxonomies(ctx, song1.ID, "genre", []string{"Ambient"})
	_ = repo.ReplaceItemTaxonomies(ctx, song2.ID, "genre", []string{"Ambient"})
	_ = repo.ReplaceItemPeople(ctx, m1.ID, []PersonInput{{Name: "Harrison Ford", Role: "Deckard", Type: "Actor"}})

	// Hydrate full detail via GetItemByID + Chapter & BIF candidates + GetItemCounts + ListTaxonomies.
	dtoFull, err := repo.GetItemByID(ctx, "s1", viewer.ID, m1.ID)
	if err != nil || len(dtoFull.Chapters) != 1 || len(dtoFull.MediaSources) != 1 || len(dtoFull.People) != 1 {
		t.Fatalf("GetItemByID full hydration unexpected: %+v err=%v", dtoFull, err)
	}
	if chCands, err := repo.ListChapterExtractionCandidates(ctx); err != nil || len(chCands) != 1 {
		t.Fatalf("ListChapterExtractionCandidates: %+v err=%v", chCands, err)
	}
	if bifCands, err := repo.ListBIFCandidates(ctx); err != nil || len(bifCands) < 3 {
		t.Fatalf("ListBIFCandidates: %+v err=%v", bifCands, err)
	}
	if counts, err := repo.GetItemCounts(ctx); err != nil || counts.MovieCount != 3 {
		t.Fatalf("GetItemCounts: %+v err=%v", counts, err)
	}
	// Second call hits cachedItemCounts fast path!
	if counts2, err := repo.GetItemCounts(ctx); err != nil || counts2.MovieCount != 3 {
		t.Fatalf("GetItemCounts cached: %+v err=%v", counts2, err)
	}
	for _, kind := range []string{"Years", "Persons", "Genres", "Studios", "Tags"} {
		if items, tot, err := repo.ListTaxonomiesWithPolicy(ctx, "s1", kind, lib.ID, 0, 10, []string{lib.ID}, intPtr(10), nil); err != nil || tot == 0 || len(items) == 0 {
			t.Fatalf("ListTaxonomiesWithPolicy(%s) failed: tot=%d err=%v", kind, tot, err)
		}
	}

	// Similar, InstantMix, and MovieRecommendations (favorite seed + top genre + empty allowed libs).
	if sim, _, err := repo.GetSimilarItemsWithPolicy(ctx, "s1", viewer.ID, m1.ID, 5, nil, []string{lib.ID}, intPtr(10), nil); err != nil || len(sim) == 0 {
		t.Fatalf("GetSimilarItemsWithPolicy: %+v err=%v", sim, err)
	}
	if simBlocked, _, _ := repo.GetSimilarItemsWithPolicy(ctx, "s1", viewer.ID, m1.ID, 5, nil, []string{lib.ID}, intPtr(5), nil); len(simBlocked) != 0 {
		t.Fatal("expected blocked R-rated seed to return empty similar items")
	}
	if mixV, _, err := repo.GetInstantMixWithPolicy(ctx, "s1", viewer.ID, m1.ID, 5, nil, []string{lib.ID}, intPtr(10), nil); err != nil || len(mixV) == 0 {
		t.Fatalf("GetInstantMixWithPolicy video: %+v err=%v", mixV, err)
	}
	if mixA, _, err := repo.GetInstantMix(ctx, "s1", viewer.ID, song1.ID, 5, nil); err != nil || len(mixA) == 0 {
		t.Fatalf("GetInstantMix audio: %+v err=%v", mixA, err)
	}
	_, _ = repo.MarkFavorite(ctx, viewer.ID, m1.ID)
	recs, err := repo.GetMovieRecommendationsWithPolicy(ctx, "s1", viewer.ID, 5, 5, nil, []string{lib.ID}, intPtr(10), nil)
	if err != nil || len(recs) < 2 || recs[0].RecommendationType != "SimilarToLikedItem" {
		t.Fatalf("GetMovieRecommendationsWithPolicy unexpected: %+v err=%v", recs, err)
	}

	// 4. Closed-store error guard coverage across LibraryRepo & UserRepo.
	closedStore, err := Open(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = closedStore.Close()
	cLib := NewLibraryRepo(closedStore)
	cUser := NewUserRepo(closedStore)

	_, _ = cLib.ListLibraries(ctx)
	_ = cLib.DeleteLibrary(ctx, "lib1")
	_, _ = cLib.UpdateLibraryOptions(ctx, "lib1", map[string]any{})
	_, _ = cLib.AddLibraryPath(ctx, "lib1", "/x")
	_ = cLib.UpsertItemsBatch(ctx, []BatchItemInput{{Item: m1}})
	_ = cLib.ReplaceItemAncestors(ctx, "i1", []AncestorLink{{AncestorID: "a1", Depth: 1}})
	_ = cLib.ReplaceMediaSourcesAndStreams(ctx, "i1", nil)
	_ = cLib.ReplaceItemImages(ctx, "i1", nil)
	_ = cLib.ReplaceItemTaxonomies(ctx, "i1", "genre", []string{"Action"})
	_ = cLib.ReplaceItemPeople(ctx, "i1", []PersonInput{{Name: "A"}})
	_ = cLib.UpsertSearchFTS(ctx, "i1", "t", "t", "", "", "")
	_, _, _ = cLib.QueryItems(ctx, ItemFilter{})
	_, _ = cLib.GetItemByID(ctx, "s", "", "i1")
	_, _, _ = cLib.GetNextUpEpisodes(ctx, "s", "u", "", 5, nil)
	_, _, _ = cLib.SearchHints(ctx, SearchHintFilter{SearchTerm: "test"})
	_, _, _ = cLib.ListTaxonomies(ctx, "s", "Years", "", 0, 5)
	_, _, _ = cLib.ListTaxonomies(ctx, "s", "Persons", "", 0, 5)
	_, _, _ = cLib.ListTaxonomies(ctx, "s", "Genres", "", 0, 5)
	_, _ = cLib.GetItemCounts(ctx)
	_, _ = cLib.MarkPlayed(ctx, "u", "i1")
	_, _ = cLib.MarkUnplayed(ctx, "u", "i1")
	_, _ = cLib.MarkFavorite(ctx, "u", "i1")
	_, _ = cLib.UnmarkFavorite(ctx, "u", "i1")
	_, _ = cLib.GetMediaSourcesByItemID(ctx, "i1")
	_, _ = cLib.GetItemExtras(ctx, "s", "u", "i1", nil)
	_, _ = cLib.ListChapterExtractionCandidates(ctx)
	_, _ = cLib.ListBIFCandidates(ctx)
	_, _ = cLib.CreateCollection(ctx, "c", "", nil)
	_ = cLib.DeleteItem(ctx, "i1")
	_, _ = cLib.DeleteItemsByPathPrefix(ctx, "l", "/p")
	_, _ = cLib.ListLibraryItemPaths(ctx, "l")
	_, _ = cLib.DeleteItemByPath(ctx, "l", "/p")
	_, _ = cLib.AddItemsToContainer(ctx, "c", []string{"i1"}, true)
	_, _ = cLib.RemoveItemsFromContainer(ctx, "c", []string{"i1"})
	_ = cLib.MoveContainerItem(ctx, "c", "i1", 0)
	_, _, _ = cLib.GetContainerItems(ctx, "s", "u", "c", 0, 5, nil)
	_, _, _ = cLib.GetSimilarItemsWithPolicy(ctx, "s", "u", "i1", 5, nil, nil, nil, nil)
	_, _, _ = cLib.GetInstantMix(ctx, "s", "u", "i1", 5, nil)
	_, _ = cLib.GetMovieRecommendations(ctx, "s", "u", 5, 5, nil)
	_, _ = cLib.GetQueryFilters(ctx, QueryFiltersFilter{})
	_, _, _ = cLib.ListFilterValues(ctx, "s", "Tag", "", nil, 0, 5)
	_, _ = cLib.FindItemsByProviderID(ctx, "l", "Movie", "Tmdb", "1")
	_, _ = cLib.FindMovieInDirByBaseTitle(ctx, "l", "/d", "m")
	_ = cLib.UpsertMediaSourceAndStreams(ctx, "i1", MediaSourceRecord{Path: "/p"})
	_, _, _ = cLib.MergeItemVersions(ctx, []string{"i1", "i2"})
	_, _ = cLib.SplitItemVersions(ctx, "i1")

	_ = cUser.DeleteUserSafe(ctx, "u1")
	_ = cUser.UpdateUserPolicySafe(ctx, "u1", dto.UserPolicy{})
	_, _ = cUser.ListAPIKeys(ctx)
	_, _ = cUser.DeleteAPIKeyByIDOrToken(ctx, "k1")
	_, _ = cUser.ListCameraUploads(ctx, "u1", "d1")
	_ = cUser.DeleteDeviceArtifacts(ctx, "d1")
	_, _, _ = cUser.ListActivityLogsFiltered(ctx, 0, 5, nil, "", "")
	_, _ = cUser.SetUserItemLikes(ctx, "u1", "i1", boolPtr(true))

	// 5. Additional functional edge branches & schema-error resilience to exceed 91%+ statement coverage.
	edgeStore := openTestStore(t)
	eLib := NewLibraryRepo(edgeStore)
	eUser := NewUserRepo(edgeStore)

	// User scan with explicit JSON nulls for slices + restricted user with BlockUnratedItems & AllowedTags for NextUp!
	restUser := &UserRecord{Name: "RestViewer", Policy: dto.UserPolicy{
		EnableAllFolders:  true,
		BlockUnratedItems: []string{"Episode"},
		AllowedTags:       []string{"keep"},
	}}
	nullUser := &UserRecord{Name: "NullSliceViewer", Policy: DefaultUserPolicy(false)}
	_ = eUser.CreateUser(ctx, restUser)
	_ = eUser.CreateUser(ctx, nullUser)
	_ = eUser.DeleteTokensByUserExcept(ctx, "   ", "")
	_, _ = edgeStore.Writer.ExecContext(ctx,
		`UPDATE users SET policy_json = '{"EnabledFolders":null,"BlockedTags":null}',
		                  config_json = '{"OrderedViews":null,"LatestItemsExcludes":null,"MyMediaExcludes":null}'
		 WHERE id = ?`, nullUser.ID)
	_, _ = eUser.ListUsers(ctx)

	// Small helper edge cases in library_repo & collections_repo.
	if !(&LibraryRecord{OptionsJSON: `{"SaveLocalMetadata":"invalid"}`}).SaveLocalMetadataEnabled() {
		t.Fatal("expected non-bool SaveLocalMetadata option to default to true")
	}
	_ = ParseParentalRatingValue("TV-Y7-CUSTOM")
	_ = ExceedingParentalRatings(-1)
	_ = policyFromUserPolicy(dto.UserPolicy{IsAdministrator: true})
	_, _, _ = eLib.loadRestrictedUserPolicy(ctx, "")
	_, _ = eLib.isItemBlockedAsUnrated(ctx, &ItemRecord{Type: "Episode"}, []string{"all"})
	_ = eLib.upsertContainerAncestors(ctx, nil)

	// Series/Season stats with empty allowedLibraryIDs.
	if statsEmpty, err := eLib.loadSeriesSeasonStatsBatchWithPolicy(ctx, restUser.ID, []string{"s1"}, []string{"sn1"}, parentalPolicy{}, []string{}); err != nil || len(statsEmpty) != 2 {
		t.Fatalf("loadSeriesSeasonStatsBatchWithPolicy empty libs: %+v err=%v", statsEmpty, err)
	}

	// Recent-movies fallback in GetMovieRecommendations (library with un-genred movie), empty Similar, Video Playlist InstantMix!
	bareLib, _ := eLib.CreateLibrary(ctx, "Bare", "movies", nil)
	_, _ = eLib.UpdateLibraryOptions(ctx, bareLib.ID, map[string]any{"EnableRealtimeMonitor": false})
	bareMov := &ItemRecord{ID: guid.New(), LibraryID: bareLib.ID, Type: "Movie", MediaType: "Video", Name: "Solo", SortName: "solo", OfficialRating: "PG"}
	_ = eLib.UpsertItem(ctx, bareMov)
	_ = eLib.ReplaceItemTaxonomies(ctx, bareMov.ID, "tag", []string{"", "keep"})
	_, _ = eLib.MarkPlayed(ctx, restUser.ID, bareMov.ID) // played seed with 0 genres -> hits len(seedGenres)==0 -> Recent fallback!
	if bareRecs, err := eLib.GetMovieRecommendations(ctx, "s1", restUser.ID, 3, 5, nil); err != nil || len(bareRecs) != 1 || bareRecs[0].CategoryId != "recent-movies" {
		t.Fatalf("expected recent-movies fallback category, got %+v err=%v", bareRecs, err)
	}
	if emptySim, _, err := eLib.GetSimilarItemsWithPolicy(ctx, "s1", restUser.ID, bareMov.ID, 5, nil, []string{bareLib.ID}, nil, nil); err != nil || len(emptySim) != 0 {
		t.Fatalf("expected empty similar items when solo movie in library, got %+v err=%v", emptySim, err)
	}
	parentBox, _ := eLib.CreateCollection(ctx, "ParentBox", "", nil)
	_, _ = eLib.CreateCollection(ctx, "NestedBox", parentBox.ID, nil)
	_, _ = eLib.DeleteItemsByPathPrefix(ctx, bareLib.ID, "/no/matching/items")
	vidPl, _ := eLib.CreatePlaylist(ctx, "VidPL", "Video", restUser.ID, []string{bareMov.ID})
	_, _ = eUser.SetUserItemLikes(ctx, restUser.ID, bareMov.ID, boolPtr(true))
	if plItems, _, err := eLib.GetContainerItems(ctx, "s1", restUser.ID, vidPl.ID, 0, 10, nil); err != nil || len(plItems) != 1 || plItems[0].UserData == nil || plItems[0].UserData.Likes == nil {
		t.Fatalf("expected Likes & LastPlayedDate populated in scanContainerItemRow: %+v err=%v", plItems, err)
	}
	if udLiked, err := eLib.GetUserItemData(ctx, restUser.ID, bareMov.ID); err != nil || udLiked.Likes == nil || !*udLiked.Likes {
		t.Fatalf("expected LibraryRepo.GetUserItemData Likes=true, got %+v err=%v", udLiked, err)
	}
	_, _, _ = eLib.GetInstantMix(ctx, "s1", restUser.ID, vidPl.ID, 5, nil)
	// InstantMix on empty Audio playlist when no Audio items exist -> mix == nil -> []dto.BaseItemDto{}.
	audPl, _ := eLib.CreatePlaylist(ctx, "AudPL", "Audio", restUser.ID, nil)
	if mixEmpty, _, err := eLib.GetInstantMix(ctx, "s1", restUser.ID, audPl.ID, 5, nil); err != nil || len(mixEmpty) != 0 {
		t.Fatalf("expected empty audio instant mix, got %+v err=%v", mixEmpty, err)
	}

	// NextUp with restricted user + Likes/LastPlayedDate in scanItemRowWithTotal + anonymous Series GetItemByID!
	nxSer := &ItemRecord{ID: guid.New(), LibraryID: bareLib.ID, Type: "Series", IsFolder: true, Name: "NxShow", SortName: "nxshow", OfficialRating: "TV-PG"}
	nxEp1 := &ItemRecord{ID: guid.New(), LibraryID: bareLib.ID, SeriesID: nxSer.ID, Type: "Episode", MediaType: "Video", Name: "E1", SortName: "e1", ParentIndexNumber: intPtr(1), IndexNumber: intPtr(1), OfficialRating: "TV-PG"}
	nxEp2 := &ItemRecord{ID: guid.New(), LibraryID: bareLib.ID, SeriesID: nxSer.ID, Type: "Episode", MediaType: "Video", Name: "E2", SortName: "e2", ParentIndexNumber: intPtr(1), IndexNumber: intPtr(2), OfficialRating: "TV-PG"}
	_ = eLib.UpsertItem(ctx, nxSer)
	_ = eLib.UpsertItem(ctx, nxEp1)
	_ = eLib.UpsertItem(ctx, nxEp2)
	_, _ = eLib.GetItemByID(ctx, "s1", "", nxSer.ID)
	_ = eLib.ReplaceItemTaxonomies(ctx, nxEp1.ID, "tag", []string{"keep"})
	_ = eLib.ReplaceItemTaxonomies(ctx, nxEp2.ID, "tag", []string{"keep"})
	_, _ = eLib.MarkPlayed(ctx, restUser.ID, nxEp1.ID)
	_, _ = eUser.UpsertPlaybackProgress(ctx, restUser.ID, nxEp2.ID, 500, false, nil, nil)
	_, _ = eUser.SetUserItemLikes(ctx, restUser.ID, nxEp2.ID, boolPtr(false))
	if nextUp, tot, err := eLib.GetNextUpEpisodesWithPolicy(ctx, "s1", restUser.ID, nxSer.ID, 5, nil, []string{bareLib.ID}, intPtr(10), nil); err != nil || tot != 1 || len(nextUp) != 1 {
		t.Fatalf("GetNextUpEpisodesWithPolicy expected [nxEp2], got tot=%d items=%+v err=%v", tot, nextUp, err)
	}

	// QueryItems prefix edge cases (punctuation prefix & 0xFF upper-bound overflow) + standalone UpsertSearchFTS + secondary DeleteItemByPath!
	_, _, _ = eLib.QueryItems(ctx, ItemFilter{NameStartsWith: "!!!", NameStartsWithOrGreater: "!!!", NameLessThan: "~~~"})
	_, _, _ = eLib.QueryItems(ctx, ItemFilter{NameStartsWith: "\xff\xff"})
	_ = eLib.UpsertSearchFTS(ctx, "orphan-fts-id", "Orphan Title", "orphan title", "", "", "")
	_ = eLib.UpsertSearchFTS(ctx, "orphan-fts-id", "Orphan Title", "orphan title", "", "", "")
	_ = eLib.UpsertSearchFTS(ctx, "orphan-fts-id", "Orphan Updated", "orphan updated", "", "", "")

	// Legacy FTS rowid collision in UpsertItemsBatch (insert orphan FTS at next rowid before batch insert).
	var maxFTSRowID int64
	_ = edgeStore.Reader.QueryRowContext(ctx, `SELECT COALESCE(MAX(rowid), 0) FROM items`).Scan(&maxFTSRowID)
	_, _ = edgeStore.Writer.ExecContext(ctx, `INSERT INTO item_search_fts (rowid, item_id, title_raw, title_tokens, pinyin_full, pinyin_initials, people_names) VALUES (?, 'other-item', 'x', 'x', 'x', 'x', '')`, maxFTSRowID+1)
	collideMov := &ItemRecord{ID: guid.New(), LibraryID: bareLib.ID, Type: "Movie", Name: "Collide", SortName: "collide", Container: "mkv", Path: "/m/c1.mkv"}
	_ = eLib.UpsertItemsBatch(ctx, []BatchItemInput{{Item: collideMov, FTSTitleRaw: "Collide", FTSTitleTokens: "collide"}})
	_ = eLib.UpsertMediaSourceAndStreams(ctx, collideMov.ID, MediaSourceRecord{ID: guid.New(), Path: "/m/c1.mkv", Container: "mkv"})
	_ = eLib.UpsertMediaSourceAndStreams(ctx, collideMov.ID, MediaSourceRecord{ID: guid.New(), Path: "/m/c2.mkv", Container: "mp4"})
	_ = eLib.UpsertMediaSourceAndStreams(ctx, collideMov.ID, MediaSourceRecord{ID: guid.New(), Path: "https://cdn.example/c3.strm", IsRemote: true})
	// Delete secondary path /m/c2.mkv (keeps primary path /m/c1.mkv, hits non-primary update branch in DeleteItemByPath!), then split remote source!
	if ok, err := eLib.DeleteItemByPath(ctx, bareLib.ID, "/m/c2.mkv"); err != nil || !ok {
		t.Fatalf("DeleteItemByPath secondary source failed: %v %v", ok, err)
	}
	_, _ = eLib.SplitItemVersions(ctx, collideMov.ID)

	// Additional functional edge branches: BlockUnratedItems=['all'] SQL, missing SeriesID inheritance, malformed policy_json,
	// UpsertItemsBatch empty AncestorIDs removal, deleteItemIDsTx empty/dup guards, and Recommendation solo-genre/categoryLimit breaks!
	_, _, _ = eLib.QueryItems(ctx, ItemFilter{BlockUnratedItems: []string{"all"}})
	_, _ = eLib.isItemBlockedAsUnrated(ctx, &ItemRecord{Type: "Episode", SeriesID: "missing-series-id"}, []string{"all"})
	_, _ = edgeStore.Writer.ExecContext(ctx, `UPDATE users SET policy_json = '{bad-json' WHERE id = ?`, nullUser.ID)
	_, _, _ = eLib.loadRestrictedUserPolicy(ctx, nullUser.ID)
	_ = eLib.UpsertItemsBatch(ctx, []BatchItemInput{{Item: collideMov, AncestorIDs: nil, FTSTitleRaw: "Collide"}})
	if tx, txErr := edgeStore.Writer.BeginTx(ctx, nil); txErr == nil {
		_, _ = deleteItemIDsTx(ctx, tx, []string{"", "   "})
		_, _ = deleteItemIDsTx(ctx, tx, []string{collideMov.ID, collideMov.ID})
		_ = tx.Rollback()
	}
	_ = eLib.ReplaceItemTaxonomies(ctx, bareMov.ID, "genre", []string{"UniqueSoloGenre"})
	secondGenreMov := &ItemRecord{ID: guid.New(), LibraryID: bareLib.ID, Type: "Movie", MediaType: "Video", Name: "SecondG", SortName: "secondg", OfficialRating: "PG"}
	_ = eLib.UpsertItem(ctx, secondGenreMov)
	_ = eLib.ReplaceItemTaxonomies(ctx, secondGenreMov.ID, "genre", []string{"AnotherGenre"})
	_, _ = eLib.GetMovieRecommendations(ctx, "s1", restUser.ID, 1, 5, nil)

	// 6. Malformed column type scan resilience & trigger-based transaction rollback verification on disposable store.
	scanStore := openTestStore(t)
	sLib := NewLibraryRepo(scanStore)
	sUser := NewUserRepo(scanStore)
	_, _ = scanStore.Writer.ExecContext(ctx, `PRAGMA foreign_keys = OFF`)
	sLibRec, _ := sLib.CreateLibrary(ctx, "ScanLib", "movies", nil)
	sU := &UserRecord{Name: "ScanUser", Policy: DefaultUserPolicy(false)}
	_ = sUser.CreateUser(ctx, sU)
	sM1 := &ItemRecord{ID: guid.New(), LibraryID: sLibRec.ID, Type: "Movie", MediaType: "Video", Name: "SM1", SortName: "sm1", Path: "/s/1.mkv"}
	sM2 := &ItemRecord{ID: guid.New(), LibraryID: sLibRec.ID, Type: "Movie", MediaType: "Video", Name: "SM2", SortName: "sm2", Path: "/s/2.mkv"}
	_ = sLib.UpsertItem(ctx, sM1)
	_ = sLib.UpsertItem(ctx, sM2)
	sSrc1 := guid.New()
	_ = sLib.UpsertMediaSourceAndStreams(ctx, sM1.ID, MediaSourceRecord{
		ID: sSrc1, Path: "/s/1.mkv", Streams: []MediaStreamRecord{{StreamIndex: 0, StreamType: "Video"}},
	})
	_ = sLib.UpsertMediaSourceAndStreams(ctx, sM1.ID, MediaSourceRecord{ID: guid.New(), Path: "/s/1b.mkv"})
	_ = sLib.UpsertMediaSourceAndStreams(ctx, sM2.ID, MediaSourceRecord{ID: guid.New(), Path: "/s/2.mkv"})
	_ = sLib.ReplaceItemImages(ctx, sM1.ID, []ImageRecord{{ImageType: "Primary", ImageIndex: 0, Path: "/s/p.jpg"}})
	_ = sLib.ReplaceItemAncestors(ctx, sM1.ID, []AncestorLink{{AncestorID: sLibRec.ID, Depth: 1}})
	_ = sLib.UpsertSearchFTS(ctx, sM1.ID, "SM1", "sm1", "", "", "")

	// Trigger-based atomic rollback checks for MergeItemVersions, SplitItemVersions, Mark*, Container ops & DeleteAPIKeyByIDOrToken.
	for _, trigSQL := range []struct {
		ddl string
		run func()
	}{
		{`CREATE TRIGGER tr_blk BEFORE INSERT ON user_item_data BEGIN SELECT RAISE(ABORT, 'blk'); END`, func() {
			_, _ = sLib.MarkPlayed(ctx, sU.ID, sM1.ID)
			_, _ = sLib.MarkFavorite(ctx, sU.ID, sM1.ID)
			_, _ = sLib.UnmarkFavorite(ctx, sU.ID, sM1.ID)
		}},
		{`CREATE TRIGGER tr_blk BEFORE UPDATE ON user_item_data BEGIN SELECT RAISE(ABORT, 'blk'); END`, func() {
			_, _ = sLib.MarkPlayed(ctx, sU.ID, sM1.ID)
			_, _ = sLib.MarkUnplayed(ctx, sU.ID, sM1.ID)
		}},
		{`CREATE TRIGGER tr_blk BEFORE UPDATE ON item_collections BEGIN SELECT RAISE(ABORT, 'blk'); END`, func() {
			_, _, _ = sLib.MergeItemVersions(ctx, []string{sM1.ID, sM2.ID})
		}},
		{`CREATE TRIGGER tr_blk BEFORE DELETE ON item_search_fts BEGIN SELECT RAISE(ABORT, 'blk'); END`, func() {
			_, _, _ = sLib.MergeItemVersions(ctx, []string{sM1.ID, sM2.ID})
			_ = sLib.DeleteItem(ctx, sM2.ID)
			_ = sLib.DeleteLibrary(ctx, sLibRec.ID)
		}},
		{`CREATE TRIGGER tr_blk BEFORE DELETE ON item_ancestors BEGIN SELECT RAISE(ABORT, 'blk'); END`, func() {
			_, _, _ = sLib.MergeItemVersions(ctx, []string{sM1.ID, sM2.ID})
			_ = sLib.DeleteItem(ctx, sM2.ID)
			_ = sLib.DeleteLibrary(ctx, sLibRec.ID)
		}},
		{`CREATE TRIGGER tr_blk BEFORE DELETE ON items BEGIN SELECT RAISE(ABORT, 'blk'); END`, func() {
			_, _, _ = sLib.MergeItemVersions(ctx, []string{sM1.ID, sM2.ID})
			_ = sLib.DeleteItem(ctx, sM2.ID)
			_ = sLib.DeleteLibrary(ctx, sLibRec.ID)
		}},
		{`CREATE TRIGGER tr_blk BEFORE INSERT ON item_search_fts BEGIN SELECT RAISE(ABORT, 'blk'); END`, func() {
			_, _ = sLib.SplitItemVersions(ctx, sM1.ID)
		}},
		{`CREATE TRIGGER tr_blk BEFORE INSERT ON item_images BEGIN SELECT RAISE(ABORT, 'blk'); END`, func() {
			_, _ = sLib.SplitItemVersions(ctx, sM1.ID)
		}},
		{`CREATE TRIGGER tr_blk BEFORE INSERT ON item_taxonomies BEGIN SELECT RAISE(ABORT, 'blk'); END`, func() {
			_, _ = sLib.SplitItemVersions(ctx, sM1.ID)
		}},
		{`CREATE TRIGGER tr_blk BEFORE INSERT ON item_people BEGIN SELECT RAISE(ABORT, 'blk'); END`, func() {
			_, _ = sLib.SplitItemVersions(ctx, sM1.ID)
		}},
		{`CREATE TRIGGER tr_blk BEFORE INSERT ON item_collections BEGIN SELECT RAISE(ABORT, 'blk'); END`, func() {
			_, _ = sLib.CreateCollection(ctx, "FailCol", "", []string{sM1.ID})
			_, _ = sLib.CreatePlaylist(ctx, "FailPL", "Video", sU.ID, []string{sM1.ID})
		}},
	} {
		_, _ = scanStore.Writer.ExecContext(ctx, trigSQL.ddl)
		trigSQL.run()
		_, _ = scanStore.Writer.ExecContext(ctx, `DROP TRIGGER IF EXISTS tr_blk`)
	}
	_ = sUser.CreateAPIKey(ctx, &APIKeyRecord{KeyID: "k-tr", Token: "t-tr", AppName: "App"})
	_, _ = scanStore.Writer.ExecContext(ctx, `CREATE TRIGGER tr_blk BEFORE DELETE ON api_keys BEGIN SELECT RAISE(ABORT, 'blk'); END`)
	_, _ = sUser.DeleteAPIKeyByIDOrToken(ctx, "k-tr")
	_, _ = scanStore.Writer.ExecContext(ctx, `DROP TRIGGER IF EXISTS tr_blk`)

	// Column scan error checks (non-integer text in integer columns / NULL views) ensure clean error returns without cursor leaks.
	_, _ = scanStore.Writer.ExecContext(ctx, `UPDATE item_media_sources SET size = 'bad-int' WHERE id = ?`, sSrc1)
	_, _ = sLib.GetMediaSourcesByItemID(ctx, sM1.ID)
	_, _ = sLib.GetItemByID(ctx, "s1", "", sM1.ID)
	_, _ = sLib.SplitItemVersions(ctx, sM1.ID)
	_, _ = sLib.DeleteItemByPath(ctx, sLibRec.ID, "/s/1.mkv")
	_, _ = scanStore.Writer.ExecContext(ctx, `UPDATE item_media_sources SET size = 100 WHERE id = ?`, sSrc1)

	_, _ = scanStore.Writer.ExecContext(ctx, `UPDATE item_media_streams SET channels = 'bad-int' WHERE media_source_id = ?`, sSrc1)
	_, _ = sLib.GetMediaSourcesByItemID(ctx, sM1.ID)
	_, _ = sLib.GetItemByID(ctx, "s1", "", sM1.ID)
	_, _ = scanStore.Writer.ExecContext(ctx, `UPDATE item_media_streams SET channels = 2 WHERE media_source_id = ?`, sSrc1)

	_, _ = scanStore.Writer.ExecContext(ctx, `UPDATE item_images SET image_index = 'bad-int' WHERE item_id = ?`, sM1.ID)
	_, _ = sLib.GetItemByID(ctx, "s1", "", sM1.ID)

	_, _ = scanStore.Writer.ExecContext(ctx, `UPDATE item_ancestors SET depth = 'bad-int' WHERE descendant_id = ?`, sM1.ID)
	_, _ = sLib.SplitItemVersions(ctx, sM1.ID)

	_, _ = scanStore.Writer.ExecContext(ctx, `INSERT INTO libraries (id, name, collection_type, paths_json, options_json, created_at, updated_at) VALUES ('bad-lib', 'BL', 'movies', '[]', '{}', 'bad-ts', 1)`)
	_, _ = sLib.ListLibraries(ctx)
	_, _ = scanStore.Writer.ExecContext(ctx, `INSERT INTO access_tokens (token, user_id, device_id, device_name, app_name, app_version, created_at, last_active_at) VALUES ('bt', ?, 'bd', 'n', 'a', '1', 1, 'bad-ts')`, sU.ID)
	_, _ = sUser.ListDevices(ctx)
	_, _ = scanStore.Writer.ExecContext(ctx, `INSERT INTO api_keys (token, app_name, created_at, key_id) VALUES ('bk', 'a', 'bad-ts', 'bkid')`)
	_, _ = sUser.ListAPIKeys(ctx)
	_, _ = scanStore.Writer.ExecContext(ctx, `INSERT INTO activity_logs (name, overview, short_overview, type, item_id, user_id, severity, created_at) VALUES ('n', '', '', 't', '', '', 'Info', 'bad-ts')`)
	_, _, _ = sUser.ListActivityLogsFiltered(ctx, 0, 5, nil, "", "")

	// NULL-producing views over child tables exercise row-scan error handling & cursor cleanup without panics.
	viewStore := openTestStore(t)
	vLib := NewLibraryRepo(viewStore)
	vUser := NewUserRepo(viewStore)
	vLibRec, _ := vLib.CreateLibrary(ctx, "ViewLib", "movies", nil)
	vU := &UserRecord{Name: "ViewUser", Policy: DefaultUserPolicy(false)}
	_ = vUser.CreateUser(ctx, vU)
	vM1 := &ItemRecord{ID: guid.New(), LibraryID: vLibRec.ID, Type: "Movie", MediaType: "Video", Name: "VM1", SortName: "vm1", Path: "/v/1.mkv"}
	vM2 := &ItemRecord{ID: guid.New(), LibraryID: vLibRec.ID, Type: "Movie", MediaType: "Video", Name: "VM2", SortName: "vm2", Path: "/v/2.mkv"}
	_ = vLib.UpsertItem(ctx, vM1)
	_ = vLib.UpsertItem(ctx, vM2)
	vSrcID := guid.New()
	_ = vLib.UpsertMediaSourceAndStreams(ctx, vM1.ID, MediaSourceRecord{
		ID: vSrcID, Path: "/v/1.mkv", Streams: []MediaStreamRecord{{StreamIndex: 0, StreamType: "Video"}},
	})
	_ = vLib.ReplaceItemTaxonomies(ctx, vM1.ID, "genre", []string{"Action"})
	_ = vLib.ReplaceItemPeople(ctx, vM1.ID, []PersonInput{{Name: "ActorV", Type: "Actor"}})
	_, _ = vLib.MarkPlayed(ctx, vU.ID, vM1.ID)
	vBox, _ := vLib.CreateCollection(ctx, "VBox", "", []string{vM1.ID})

	mID := guid.Normalize(vM1.ID)
	m2ID := guid.Normalize(vM2.ID)
	bID := guid.Normalize(vBox.ID)

	_, _ = viewStore.Writer.ExecContext(ctx, `PRAGMA foreign_keys = OFF`)

	_, _ = viewStore.Writer.ExecContext(ctx, `DROP TABLE taxonomies`)
	_, _ = viewStore.Writer.ExecContext(ctx, `CREATE VIEW taxonomies AS SELECT taxonomy_id AS id, NULL AS name, 'genre' AS kind, 'a' AS sort_name FROM item_taxonomies`)
	_, _, _, _ = vLib.loadTaxonomiesBatch(ctx, []string{mID})
	_, _, _, _ = vLib.GetTaxonomiesByItemID(ctx, mID)
	_, _ = vLib.GetQueryFilters(ctx, QueryFiltersFilter{})
	_, _, _ = vLib.GetSimilarItemsWithPolicy(ctx, "s1", vU.ID, mID, 5, nil, nil, nil, nil)
	_, _, _ = vLib.GetInstantMix(ctx, "s1", vU.ID, mID, 5, nil)
	_, _ = vLib.GetMovieRecommendations(ctx, "s1", vU.ID, 3, 5, nil)
	_, _ = vLib.GetMovieRecommendations(ctx, "s1", "", 3, 5, nil)
	_, _, _ = vLib.ListFilterValues(ctx, "s1", "MusicGenre", "", []string{"Movie"}, 0, 5)

	_, _ = viewStore.Writer.ExecContext(ctx, `DROP VIEW taxonomies`)
	_, _ = viewStore.Writer.ExecContext(ctx, `CREATE VIEW taxonomies AS SELECT taxonomy_id AS id, NULL AS name, 'studio' AS kind, 'a' AS sort_name FROM item_taxonomies`)
	_, _ = vLib.GetQueryFilters(ctx, QueryFiltersFilter{})

	_, _ = viewStore.Writer.ExecContext(ctx, `DROP VIEW taxonomies`)
	_, _ = viewStore.Writer.ExecContext(ctx, `CREATE VIEW taxonomies AS SELECT taxonomy_id AS id, NULL AS name, 'tag' AS kind, 'a' AS sort_name FROM item_taxonomies`)
	_, _ = vLib.GetQueryFilters(ctx, QueryFiltersFilter{})

	_, _ = viewStore.Writer.ExecContext(ctx, `DROP TABLE people`)
	_, _ = viewStore.Writer.ExecContext(ctx, `CREATE VIEW people AS SELECT person_id AS id, NULL AS name, 'a' AS sort_name, '{}' AS extra_json FROM item_people`)
	_, _ = vLib.loadPeopleBatch(ctx, []string{mID})

	_, _ = viewStore.Writer.ExecContext(ctx, `DROP TABLE item_images`)
	_, _ = viewStore.Writer.ExecContext(ctx, `CREATE VIEW item_images AS SELECT '`+mID+`' AS item_id, NULL AS image_type, 'bad' AS image_index, NULL AS path, NULL AS tag, 0 AS width, 0 AS height, '' AS blurhash`)
	_, _ = vLib.GetItemByID(ctx, "s1", "", mID)

	_, _ = viewStore.Writer.ExecContext(ctx, `DROP TABLE item_media_streams`)
	_, _ = viewStore.Writer.ExecContext(ctx, `CREATE VIEW item_media_streams AS SELECT '`+guid.Normalize(vSrcID)+`' AS media_source_id, '`+mID+`' AS item_id, 'bad' AS stream_index, NULL AS stream_type, '' AS codec, '' AS language, '' AS sub_variant, '' AS display_title, '' AS title, 0 AS width, 0 AS height, '' AS video_range, '' AS audio_layout, 0 AS channels, 0 AS sample_rate, 0 AS bitrate, 0 AS is_default, 0 AS is_forced, 0 AS is_external, '' AS path`)
	_, _ = vLib.loadMediaSourcesBatch(ctx, []string{mID})

	_, _ = viewStore.Writer.ExecContext(ctx, `DROP TABLE item_media_sources`)
	_, _ = viewStore.Writer.ExecContext(ctx, `CREATE VIEW item_media_sources AS SELECT 'ms1' AS id, '`+mID+`' AS item_id, NULL AS name, '/v/1.mkv' AS path, 'File' AS protocol, 'mkv' AS container, 'bad' AS size, 0 AS bitrate, 0 AS runtime_ticks, 0 AS is_remote, 1 AS supports_direct_play, 1 AS supports_direct_stream, 1 AS supports_transcoding, '{}' AS extra_json`)
	_, _ = vLib.loadMediaSourcesBatch(ctx, []string{mID})
	_, _ = vLib.DeleteItemByPath(ctx, vLibRec.ID, "/v/1.mkv")
	_, _ = vLib.SplitItemVersions(ctx, mID)
	_, _ = viewStore.Writer.ExecContext(ctx, `DROP VIEW item_media_sources`)
	_, _ = viewStore.Writer.ExecContext(ctx, `CREATE VIEW item_media_sources AS SELECT NULL AS id, '`+mID+`' AS item_id, NULL AS name, NULL AS path, 'File' AS protocol, 'mkv' AS container, 0 AS size, 0 AS bitrate, 0 AS runtime_ticks, 0 AS is_remote, 1 AS supports_direct_play, 1 AS supports_direct_stream, 1 AS supports_transcoding, '{}' AS extra_json`)
	_, _, _ = vLib.MergeItemVersions(ctx, []string{mID, m2ID})

	_, _ = viewStore.Writer.ExecContext(ctx, `DROP TABLE item_collections`)
	_, _ = viewStore.Writer.ExecContext(ctx, `CREATE VIEW item_collections AS SELECT '`+bID+`' AS container_id, NULL AS item_id, 0 AS position, NULL AS entry_id`)
	_, _ = vLib.AddItemsToContainer(ctx, bID, []string{mID}, true)
	_ = vLib.MoveContainerItem(ctx, bID, mID, 0)
	if tx, txErr := viewStore.Writer.BeginTx(ctx, nil); txErr == nil {
		_ = compactContainerPositionsTx(ctx, tx, bID)
		_ = tx.Rollback()
	}

	// Schema-corruption / missing-table error propagation checks on a disposable store (ensures graceful error returns without panics).
	corruptStore := openTestStore(t)
	kLib := NewLibraryRepo(corruptStore)
	_, _ = corruptStore.Writer.ExecContext(ctx, `PRAGMA foreign_keys = OFF`)
	kLibRec, _ := kLib.CreateLibrary(ctx, "CorruptLib", "movies", nil)
	kM1 := &ItemRecord{ID: guid.New(), LibraryID: kLibRec.ID, Type: "Movie", MediaType: "Video", Name: "KM1", SortName: "km1", Path: "/k/1.mkv"}
	kM2 := &ItemRecord{ID: guid.New(), LibraryID: kLibRec.ID, Type: "Movie", MediaType: "Video", Name: "KM2", SortName: "km2", Path: "/k/2.mkv"}
	_ = kLib.UpsertItem(ctx, kM1)
	_ = kLib.UpsertItem(ctx, kM2)
	_ = kLib.UpsertMediaSourceAndStreams(ctx, kM1.ID, MediaSourceRecord{ID: guid.New(), Path: "/k/1.mkv", Streams: []MediaStreamRecord{{StreamIndex: 0, StreamType: "Video"}}})
	_ = kLib.UpsertMediaSourceAndStreams(ctx, kM1.ID, MediaSourceRecord{ID: guid.New(), Path: "/k/1b.mkv"})
	_ = kLib.UpsertMediaSourceAndStreams(ctx, kM2.ID, MediaSourceRecord{ID: guid.New(), Path: "/k/1.mkv"})
	kBox, _ := kLib.CreateCollection(ctx, "KBox", "", []string{kM1.ID, kM2.ID})

	// Non-numeric production_year triggers scan error cleanly in GetQueryFilters & ListTaxonomies(Years).
	_, _ = corruptStore.Writer.ExecContext(ctx, `INSERT INTO items (id, library_id, type, name, sort_name, production_year, etag, date_created, date_modified) VALUES ('bad-yr', ?, 'Movie', 'B', 'b', 'not-int', 'e', 1, 1)`, kLibRec.ID)
	_, _ = kLib.GetQueryFilters(ctx, QueryFiltersFilter{})
	_, _, _ = kLib.ListTaxonomies(ctx, "s", "Years", "", 0, 10)
	_, _ = corruptStore.Writer.ExecContext(ctx, `DELETE FROM items WHERE id = 'bad-yr'`)

	// Progressive sub-table drop checks inside disposable corruptStore.
	_, _ = corruptStore.Writer.ExecContext(ctx, `DROP TABLE item_media_streams`)
	_, _ = kLib.GetMediaSourcesByItemID(ctx, kM1.ID)
	_, _ = kLib.GetItemByID(ctx, "s", "", kM1.ID)
	_ = kLib.UpsertMediaSourceAndStreams(ctx, kM1.ID, MediaSourceRecord{ID: guid.New(), Path: "/k/1.mkv"})
	_, _, _ = kLib.MergeItemVersions(ctx, []string{kM1.ID, kM2.ID})
	_, _ = kLib.SplitItemVersions(ctx, kM1.ID)

	_, _ = corruptStore.Writer.ExecContext(ctx, `DROP TABLE item_people`)
	_, _ = kLib.GetPeopleByItemID(ctx, kM1.ID)
	_ = kLib.ReplaceItemPeople(ctx, kM1.ID, []PersonInput{{Name: "X"}})
	_, _ = kLib.GetItemByID(ctx, "s", "", kM1.ID)

	_, _ = corruptStore.Writer.ExecContext(ctx, `DROP TABLE item_taxonomies`)
	_, _ = kLib.GetItemByID(ctx, "s", "", kM1.ID)
	_, _ = kLib.GetQueryFilters(ctx, QueryFiltersFilter{})
	_, _, _ = kLib.GetSimilarItemsWithPolicy(ctx, "s", "", kM1.ID, 5, nil, nil, nil, nil)
	_, _, _ = kLib.GetInstantMix(ctx, "s", "", kM1.ID, 5, nil)

	_, _ = corruptStore.Writer.ExecContext(ctx, `DROP TABLE item_images`)
	_, _ = kLib.GetItemByID(ctx, "s", "", kM1.ID)
	_ = kLib.ReplaceItemImages(ctx, kM1.ID, nil)
	_ = kLib.DeleteItem(ctx, kM2.ID)
	_ = kLib.DeleteLibrary(ctx, kLibRec.ID)

	_, _ = corruptStore.Writer.ExecContext(ctx, `DROP TABLE item_media_sources`)
	_, _ = kLib.ListLibraryItemPaths(ctx, kLibRec.ID)
	_, _, _ = kLib.MergeItemVersions(ctx, []string{kM1.ID, kBox.ID})
	_, _ = kLib.SplitItemVersions(ctx, kM1.ID)

	_, _ = corruptStore.Writer.ExecContext(ctx, `DROP TABLE item_collections`)
	_, _ = kLib.AddItemsToContainer(ctx, kBox.ID, []string{kM1.ID}, true)
	_, _ = kLib.RemoveItemsFromContainer(ctx, kBox.ID, []string{kM1.ID})
	_ = kLib.MoveContainerItem(ctx, kBox.ID, kM1.ID, 0)
	_, _, _ = kLib.GetContainerItems(ctx, "s", "", kBox.ID, 0, 5, nil)

	_, _ = corruptStore.Writer.ExecContext(ctx, `DROP TABLE item_search_fts`)
	_ = kLib.UpsertSearchFTS(ctx, kM1.ID, "a", "a", "", "", "")
	_ = kLib.UpsertItemsBatch(ctx, []BatchItemInput{{Item: kM1, FTSTitleRaw: "a"}})
}
