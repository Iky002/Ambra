package scanner

import (
	"math"
	"sort"
	"time"
)

// LibraryScanProgress tracks real-time scan progress for a single library.
type LibraryScanProgress struct {
	LibraryID     string    `json:"library_id"`
	LibraryName   string    `json:"library_name"`
	Phase         string    `json:"phase"` // "discovering" | "enriching" | "completed"
	Total         int       `json:"total"`
	Processed     int       `json:"processed"`
	Added         int       `json:"added"`
	Updated       int       `json:"updated"`
	Skipped       int       `json:"skipped"`
	Failed        int       `json:"failed"`
	CurrentItem   string    `json:"current_item"`
	CurrentAction string    `json:"current_action"`
	Percentage    float64   `json:"percentage"`
	StartedAt     time.Time `json:"started_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ScanProgressSnapshot aggregates live scan progress across all active libraries.
type ScanProgressSnapshot struct {
	Running       bool                  `json:"running"`
	Phase         string                `json:"phase"` // "idle" | "discovering" | "enriching"
	LibraryID     string                `json:"library_id,omitempty"`
	LibraryName   string                `json:"library_name,omitempty"`
	Total         int                   `json:"total"`
	Processed     int                   `json:"processed"`
	Added         int                   `json:"added"`
	Updated       int                   `json:"updated"`
	Skipped       int                   `json:"skipped"`
	Failed        int                   `json:"failed"`
	CurrentItem   string                `json:"current_item,omitempty"`
	CurrentAction string                `json:"current_action,omitempty"`
	Percentage    float64               `json:"percentage"`
	StartedAt     *time.Time            `json:"started_at,omitempty"`
	UpdatedAt     time.Time             `json:"updated_at"`
	Libraries     []LibraryScanProgress `json:"libraries,omitempty"`
}

func (s *Scanner) startLibraryProgress(libraryID, libraryName string) {
	now := time.Now()
	s.mu.Lock()
	if s.progress == nil {
		s.progress = make(map[string]*LibraryScanProgress)
	}
	s.progress[libraryID] = &LibraryScanProgress{
		LibraryID:     libraryID,
		LibraryName:   libraryName,
		Phase:         "discovering",
		CurrentAction: "正在扫描媒体文件",
		StartedAt:     now,
		UpdatedAt:     now,
	}
	s.mu.Unlock()
}

func (s *Scanner) recordDiscoveryItem(libraryID, itemName string, added, skipped, failed bool) {
	now := time.Now()
	s.mu.Lock()
	if p := s.progress[libraryID]; p != nil {
		p.CurrentItem = itemName
		p.Total++
		p.Processed++
		if added {
			p.Added++
		}
		if skipped {
			p.Skipped++
		}
		if failed {
			p.Failed++
		}
		p.UpdatedAt = now
	}
	s.mu.Unlock()
}

func (s *Scanner) startEnrichPhase(libraryID string, totalTasks int) {
	now := time.Now()
	s.mu.Lock()
	if p := s.progress[libraryID]; p != nil {
		p.Phase = "enriching"
		p.Total = totalTasks
		p.Processed = 0
		p.Percentage = 0
		p.CurrentAction = "正在刮削元数据与下载海报"
		p.UpdatedAt = now
	}
	s.mu.Unlock()
}

func (s *Scanner) updateEnrichCurrent(libraryID, itemName, action string) {
	now := time.Now()
	s.mu.Lock()
	if p := s.progress[libraryID]; p != nil {
		if itemName != "" {
			p.CurrentItem = itemName
		}
		if action != "" {
			p.CurrentAction = action
		}
		p.UpdatedAt = now
	}
	s.mu.Unlock()
}

func (s *Scanner) recordEnrichDone(libraryID string, updated bool) {
	now := time.Now()
	s.mu.Lock()
	if p := s.progress[libraryID]; p != nil {
		p.Processed++
		if updated {
			p.Updated++
		}
		if p.Total > 0 {
			pct := float64(p.Processed) / float64(p.Total) * 100
			if pct > 100 {
				pct = 100
			}
			p.Percentage = math.Round(pct*10) / 10
		}
		p.UpdatedAt = now
	}
	s.mu.Unlock()
}

func (s *Scanner) finishLibraryProgress(libraryID string) {
	s.mu.Lock()
	delete(s.progress, libraryID)
	s.mu.Unlock()
}

// Progress returns a thread-safe snapshot of current scan progress.
func (s *Scanner) Progress() ScanProgressSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.progress) == 0 {
		return ScanProgressSnapshot{
			Running:   false,
			Phase:     "idle",
			UpdatedAt: time.Now(),
		}
	}

	libs := make([]LibraryScanProgress, 0, len(s.progress))
	for _, p := range s.progress {
		libs = append(libs, *p)
	}
	sort.Slice(libs, func(i, j int) bool {
		if !libs[i].StartedAt.Equal(libs[j].StartedAt) {
			return libs[i].StartedAt.Before(libs[j].StartedAt)
		}
		return libs[i].LibraryID < libs[j].LibraryID
	})

	snap := ScanProgressSnapshot{
		Running:   true,
		Phase:     "enriching",
		Libraries: libs,
	}

	var earliestStart time.Time
	var latestUpdate time.Time
	var primary *LibraryScanProgress

	for i := range libs {
		lp := &libs[i]
		if lp.Phase == "discovering" {
			snap.Phase = "discovering"
		}
		snap.Total += lp.Total
		snap.Processed += lp.Processed
		snap.Added += lp.Added
		snap.Updated += lp.Updated
		snap.Skipped += lp.Skipped
		snap.Failed += lp.Failed

		if earliestStart.IsZero() || lp.StartedAt.Before(earliestStart) {
			earliestStart = lp.StartedAt
		}
		if latestUpdate.IsZero() || lp.UpdatedAt.After(latestUpdate) {
			latestUpdate = lp.UpdatedAt
			primary = lp
		}
	}

	if primary != nil {
		snap.LibraryID = primary.LibraryID
		snap.LibraryName = primary.LibraryName
		snap.CurrentItem = primary.CurrentItem
		snap.CurrentAction = primary.CurrentAction
	}
	if !earliestStart.IsZero() {
		t := earliestStart
		snap.StartedAt = &t
	}
	snap.UpdatedAt = latestUpdate

	if snap.Phase == "enriching" && snap.Total > 0 {
		pct := float64(snap.Processed) / float64(snap.Total) * 100
		if pct > 100 {
			pct = 100
		}
		snap.Percentage = math.Round(pct*10) / 10
	}

	return snap
}
