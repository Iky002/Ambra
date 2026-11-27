package scheduler

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuditV0112_DynamicTranscodeTempDirCleanCache(t *testing.T) {
	staticDir := t.TempDir()
	dynDir1 := t.TempDir()
	dynDir2 := t.TempDir()

	writeStaleFile := func(dir, name string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("ts-segment-bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-4 * time.Hour)
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		return p
	}

	mgr := New(nil)
	mgr.RegisterBuiltinTasks(nil, staticDir, nil)

	// 1. fn == nil -> falls back to static transcodeTempDir; stale file deleted, recent (<2h) file preserved.
	fStatic1 := writeStaleFile(staticDir, "static-1.ts")
	fRecent := filepath.Join(staticDir, "recent-active.ts")
	if err := os.WriteFile(fRecent, []byte("recent-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Run("CleanTranscodeCache"); err != nil {
		t.Fatalf("Run CleanTranscodeCache (nil fn): %v", err)
	}
	mgr.Wait("CleanTranscodeCache")
	if _, err := os.Stat(fStatic1); !os.IsNotExist(err) {
		t.Fatalf("expected stale static file removed when fn is nil, stat err=%v", err)
	}
	if _, err := os.Stat(fRecent); err != nil {
		t.Fatalf("expected recent (<2h) top-level file preserved, err=%v", err)
	}
	_ = os.Remove(fRecent)

	// 2. fn returns "" -> falls back to static transcodeTempDir.
	var currentDir atomic.Value
	currentDir.Store("")
	mgr.SetTranscodeTempDirFn(func() string {
		v, _ := currentDir.Load().(string)
		return v
	})
	fStatic2 := writeStaleFile(staticDir, "static-2.ts")
	if err := mgr.Run("CleanTranscodeCache"); err != nil {
		t.Fatalf("Run CleanTranscodeCache (empty fn): %v", err)
	}
	mgr.Wait("CleanTranscodeCache")
	if _, err := os.Stat(fStatic2); !os.IsNotExist(err) {
		t.Fatalf("expected static file removed when fn returns empty string, stat err=%v", err)
	}

	// 3. fn returns dynDir1 -> cleans dynDir1 while leaving staticDir untouched, then switches to dynDir2 on next run.
	fStaticKeep := writeStaleFile(staticDir, "static-keep.ts")
	fDyn1 := writeStaleFile(dynDir1, "dyn-1.ts")
	fDyn2 := writeStaleFile(dynDir2, "dyn-2.ts")

	currentDir.Store(dynDir1)
	if err := mgr.Run("CleanTranscodeCache"); err != nil {
		t.Fatalf("Run CleanTranscodeCache (dynDir1): %v", err)
	}
	mgr.Wait("CleanTranscodeCache")
	if _, err := os.Stat(fDyn1); !os.IsNotExist(err) {
		t.Fatalf("expected dynDir1 file removed, stat err=%v", err)
	}
	if _, err := os.Stat(fStaticKeep); err != nil {
		t.Fatalf("expected staticDir file untouched while dynDir1 active, err=%v", err)
	}
	if _, err := os.Stat(fDyn2); err != nil {
		t.Fatalf("expected dynDir2 file untouched while dynDir1 active, err=%v", err)
	}

	currentDir.Store(dynDir2)
	if err := mgr.Run("CleanTranscodeCache"); err != nil {
		t.Fatalf("Run CleanTranscodeCache (dynDir2): %v", err)
	}
	mgr.Wait("CleanTranscodeCache")
	if _, err := os.Stat(fDyn2); !os.IsNotExist(err) {
		t.Fatalf("expected dynDir2 file removed after dynamic switch, stat err=%v", err)
	}

	// 4. Unsafe target directories (os.TempDir() root, "/", system dirs, UserHomeDir) must be rejected without deleting anything!
	canaryInTempRoot, err := os.CreateTemp(os.TempDir(), "ambra-scheduler-canary-*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	canaryPath := canaryInTempRoot.Name()
	_ = canaryInTempRoot.Close()
	t.Cleanup(func() { _ = os.Remove(canaryPath) })
	old := time.Now().Add(-4 * time.Hour)
	_ = os.Chtimes(canaryPath, old, old)

	currentDir.Store(os.TempDir())
	if err := mgr.Run("CleanTranscodeCache"); err != nil {
		t.Fatalf("Run CleanTranscodeCache (os.TempDir()): %v", err)
	}
	mgr.Wait("CleanTranscodeCache")
	if info, ok := mgr.Get("CleanTranscodeCache"); !ok || info.LastExecutionResult == nil || info.LastExecutionResult.Status != "Failed" {
		t.Fatalf("expected CleanTranscodeCache on os.TempDir() root to fail safety check, got %+v", info)
	}
	if _, err := os.Stat(canaryPath); err != nil {
		t.Fatalf("expected canary file in os.TempDir() root to be untouched, err=%v", err)
	}

	for _, unsafeDir := range []string{"/", "/etc", "/usr", "/var", "/bin", "/sbin", "/lib", "/home", "/root", os.TempDir()} {
		if err := validateTranscodeCleanTargetDir(unsafeDir); err == nil {
			t.Fatalf("expected validateTranscodeCleanTargetDir(%q) to fail", unsafeDir)
		}
	}
	if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
		if err := validateTranscodeCleanTargetDir(homeDir); err == nil {
			t.Fatalf("expected validateTranscodeCleanTargetDir(homeDir=%q) to fail", homeDir)
		}
	}
	if err := validateTranscodeCleanTargetDir(dynDir1); err != nil {
		t.Fatalf("expected subdirectory inside os.TempDir() (%q) to be allowed: %v", dynDir1, err)
	}

	// 5. Prefix-based system directory protection rejects descendants too.
	for _, unsafeDesc := range []string{"/etc/ambra", "/usr/local/bin", "/var/lib/ambra", "/var/log/ambra", "/proc/self", "/sys/fs", "/dev/shm"} {
		if err := validateTranscodeCleanTargetDir(unsafeDesc); err == nil {
			t.Fatalf("expected validateTranscodeCleanTargetDir(%q) to reject system descendant", unsafeDesc)
		}
	}

	// 6. DataDir & media library root overlap protection.
	dataDir := filepath.Join(t.TempDir(), "data")
	allowedTranscodeSub := filepath.Join(dataDir, "cache", "transcoding-temp")
	forbiddenDataSub := filepath.Join(dataDir, "db")
	mediaRoot := filepath.Join(t.TempDir(), "movies")
	mediaSub := filepath.Join(mediaRoot, "action")
	for _, d := range []string{allowedTranscodeSub, forbiddenDataSub, mediaSub} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := validateTranscodeCleanTargetDirWithScope(allowedTranscodeSub, dataDir, []string{mediaRoot}); err != nil {
		t.Fatalf("expected DataDir/cache/transcoding-temp to be allowed, got: %v", err)
	}
	for _, bad := range []string{dataDir, filepath.Dir(dataDir), forbiddenDataSub, mediaRoot, mediaSub, filepath.Dir(mediaRoot)} {
		if err := validateTranscodeCleanTargetDirWithScope(bad, dataDir, []string{mediaRoot}); err == nil {
			t.Fatalf("expected validateTranscodeCleanTargetDirWithScope(%q) to fail", bad)
		}
	}
}

