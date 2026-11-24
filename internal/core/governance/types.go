package governance

import "errors"

var (
	ErrNotFound        = errors.New("governance record not found")
	ErrInvalidArgument = errors.New("invalid argument")
	ErrItemPathEmpty   = errors.New("item has no local path")
)

const (
	AuditStatusPending   = "pending"
	AuditStatusApproved  = "approved"
	AuditStatusDismissed = "dismissed"
)

// ScrapeCandidate represents a metadata match candidate for a low-confidence scrape item.
type ScrapeCandidate struct {
	ProviderID    string   `json:"ProviderId"`
	ProviderName  string   `json:"ProviderName,omitempty"`
	Title         string   `json:"Title"`
	OriginalTitle string   `json:"OriginalTitle,omitempty"`
	Year          *int     `json:"Year,omitempty"`
	Overview      string   `json:"Overview,omitempty"`
	Rating        *float64 `json:"Rating,omitempty"`
	PosterURL     string   `json:"PosterUrl,omitempty"`
	Confidence    float64  `json:"Confidence"`
	Genres        []string `json:"Genres,omitempty"`
	Tags          []string `json:"Tags,omitempty"`
}

// ScrapeAuditItem represents a queued low-confidence scrape item awaiting admin review.
type ScrapeAuditItem struct {
	ID         string            `json:"Id"`
	ItemID     string            `json:"ItemId"`
	ItemName   string            `json:"ItemName"`
	ItemType   string            `json:"ItemType"`
	Path       string            `json:"Path"`
	Confidence float64           `json:"Confidence"`
	Candidates []ScrapeCandidate `json:"Candidates"`
	Status     string            `json:"Status"`
	CreatedAt  int64             `json:"CreatedAt"`
	UpdatedAt  int64             `json:"UpdatedAt"`
}

// EnqueueAuditInput holds the fields required to add an item to the scrape audit queue.
type EnqueueAuditInput struct {
	ItemID     string
	ItemName   string
	ItemType   string
	Path       string
	Confidence float64
	Candidates []ScrapeCandidate
}

// LockedFieldsResult is returned after updating or querying locked metadata fields for an item.
type LockedFieldsResult struct {
	ItemID       string   `json:"ItemId"`
	LockedFields []string `json:"LockedFields"`
	LockData     bool     `json:"LockData"`
}

// NFOWriteResult summarizes the result of an atomic NFO writeback.
type NFOWriteResult struct {
	ItemID    string `json:"ItemId"`
	NFOPath   string `json:"NfoPath"`
	RootTag   string `json:"RootTag"`
	BytesSize int    `json:"BytesSize"`
}

// AuditIssueItem describes a single media item flagged during a library health audit.
type AuditIssueItem struct {
	ID        string `json:"Id"`
	Name      string `json:"Name"`
	Type      string `json:"Type"`
	LibraryID string `json:"LibraryId"`
	Path      string `json:"Path"`
	Year      *int   `json:"Year,omitempty"`
	IsStrm    bool   `json:"IsStrm"`
}

// LibraryAuditReport summarizes missing posters, missing TMDB IDs, and orphaned disk records.
type LibraryAuditReport struct {
	ScannedAt      int64            `json:"ScannedAt"`
	TotalChecked   int              `json:"TotalChecked"`
	MissingPosters []AuditIssueItem `json:"MissingPosters"`
	MissingTMDB    []AuditIssueItem `json:"MissingTmdb"`
	OrphanItems    []AuditIssueItem `json:"OrphanItems"`
}

// OrphanCleanupResult summarizes a batch orphan cleanup operation.
type OrphanCleanupResult struct {
	Requested  int      `json:"Requested"`
	Deleted    int      `json:"Deleted"`
	DeletedIDs []string `json:"DeletedIds"`
}

// MemoryHealth captures Go runtime memory statistics.
type MemoryHealth struct {
	AllocBytes      uint64 `json:"AllocBytes"`
	TotalAllocBytes uint64 `json:"TotalAllocBytes"`
	SysBytes        uint64 `json:"SysBytes"`
	HeapInuseBytes  uint64 `json:"HeapInuseBytes"`
	NumGC           uint32 `json:"NumGC"`
}

// DatabaseHealth captures SQLite primary, WAL, and SHM file sizes.
type DatabaseHealth struct {
	DBPath     string `json:"DbPath"`
	DBBytes    int64  `json:"DbBytes"`
	WALBytes   int64  `json:"WalBytes"`
	SHMBytes   int64  `json:"ShmBytes"`
	TotalBytes int64  `json:"TotalBytes"`
}

// CacheHealth captures disk usage across cache directories.
type CacheHealth struct {
	ImagesBytes    int64 `json:"ImagesBytes"`
	SubtitlesBytes int64 `json:"SubtitlesBytes"`
	TranscodeBytes int64 `json:"TranscodeBytes"`
	TotalBytes     int64 `json:"TotalBytes"`
}

// DiskHealth captures filesystem capacity and free space for the data directory.
type DiskHealth struct {
	Path           string  `json:"Path"`
	TotalBytes     uint64  `json:"TotalBytes"`
	FreeBytes      uint64  `json:"FreeBytes"`
	AvailableBytes uint64  `json:"AvailableBytes"`
	UsedPercent    float64 `json:"UsedPercent"`
}

// LibraryStatsHealth captures aggregate item, user, and pending audit counts.
type LibraryStatsHealth struct {
	TotalItems    int `json:"TotalItems"`
	MovieCount    int `json:"MovieCount"`
	SeriesCount   int `json:"SeriesCount"`
	EpisodeCount  int `json:"EpisodeCount"`
	UserCount     int `json:"UserCount"`
	PendingAudits int `json:"PendingAudits"`
}

// SchedulerHealth captures active scheduled tasks and background budget utilization.
type SchedulerHealth struct {
	TotalTasks     int     `json:"TotalTasks"`
	ActiveTasks    int     `json:"ActiveTasks"`
	BudgetInUse    int64   `json:"BudgetInUse"`
	BudgetCapacity int64   `json:"BudgetCapacity"`
	BudgetUsagePct float64 `json:"BudgetUsagePct"`
}

// SystemHealthReport is the complete real-time server health diagnostic snapshot.
type SystemHealthReport struct {
	CollectedAt  int64              `json:"CollectedAt"`
	Status       string             `json:"Status"`
	GoVersion    string             `json:"GoVersion"`
	NumCPU       int                `json:"NumCpu"`
	NumGoroutine int                `json:"NumGoroutine"`
	Memory       MemoryHealth       `json:"Memory"`
	Database     DatabaseHealth     `json:"Database"`
	Cache        CacheHealth        `json:"Cache"`
	Disk         DiskHealth         `json:"Disk"`
	Library      LibraryStatsHealth `json:"Library"`
	Scheduler    SchedulerHealth    `json:"Scheduler"`
}
