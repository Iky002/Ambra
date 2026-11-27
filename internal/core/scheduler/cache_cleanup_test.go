package scheduler

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestAuditV0112_DynamicTranscodeTempDirCleanCache(t *testing.T) {
	staticDir := t.TempDir()
	dynDir1 := t.TempDir()
	dynDir2 := t.TempDir()

	writeFile := func(dir, name string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("ts-segment-bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}

	mgr := New(nil)
	mgr.RegisterBuiltinTasks(nil, staticDir, nil)

	// 1. fn == nil -> falls back to static transcodeTempDir.
	fStatic1 := writeFile(staticDir, "static-1.ts")
	if err := mgr.Run("CleanTranscodeCache"); err != nil {
		t.Fatalf("Run CleanTranscodeCache (nil fn): %v", err)
	}
	mgr.Wait("CleanTranscodeCache")
	if _, err := os.Stat(fStatic1); !os.IsNotExist(err) {
		t.Fatalf("expected static file removed when fn is nil, stat err=%v", err)
	}

	// 2. fn returns "" -> falls back to static transcodeTempDir.
	var currentDir atomic.Value
	currentDir.Store("")
	mgr.SetTranscodeTempDirFn(func() string {
		v, _ := currentDir.Load().(string)
		return v
	})
	fStatic2 := writeFile(staticDir, "static-2.ts")
	if err := mgr.Run("CleanTranscodeCache"); err != nil {
		t.Fatalf("Run CleanTranscodeCache (empty fn): %v", err)
	}
	mgr.Wait("CleanTranscodeCache")
	if _, err := os.Stat(fStatic2); !os.IsNotExist(err) {
		t.Fatalf("expected static file removed when fn returns empty string, stat err=%v", err)
	}

	// 3. fn returns dynDir1 -> cleans dynDir1 while leaving staticDir untouched, then switches to dynDir2 on next run.
	fStaticKeep := writeFile(staticDir, "static-keep.ts")
	fDyn1 := writeFile(dynDir1, "dyn-1.ts")
	fDyn2 := writeFile(dynDir2, "dyn-2.ts")

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
}
