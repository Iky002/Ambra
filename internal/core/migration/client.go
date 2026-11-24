package migration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ambra-media/ambra/internal/api/dto"
)

const (
	defaultHTTPTimeout = 20 * time.Second
	defaultMaxRetries  = 4
	defaultBaseBackoff = 250 * time.Millisecond
	maxBackoffDelay    = 10 * time.Second
)

// APIError represents a non-2xx HTTP response returned by the source Emby server.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("source emby returned HTTP %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("source emby returned HTTP %d", e.StatusCode)
}

// SourceServerInfo holds basic metadata returned by GET /System/Info.
type SourceServerInfo struct {
	ID              string `json:"Id"`
	ServerName      string `json:"ServerName"`
	Version         string `json:"Version"`
	ProductName     string `json:"ProductName"`
	OperatingSystem string `json:"OperatingSystem"`
	LocalAddress    string `json:"LocalAddress"`
	Status          string `json:"Status"`
}

// SourceUser represents a user entry returned by GET /Users on the source Emby server.
type SourceUser struct {
	ID               string                `json:"Id"`
	Name             string                `json:"Name"`
	HasPassword      bool                  `json:"HasPassword"`
	LastLoginDate    *time.Time            `json:"LastLoginDate,omitempty"`
	LastActivityDate *time.Time            `json:"LastActivityDate,omitempty"`
	Policy           dto.UserPolicy        `json:"Policy"`
	Configuration    dto.UserConfiguration `json:"Configuration"`
}

// SourceVirtualFolder represents a library returned by GET /Library/VirtualFolders.
type SourceVirtualFolder struct {
	Name           string   `json:"Name"`
	ItemID         string   `json:"ItemId"`
	CollectionType string   `json:"CollectionType"`
	Locations      []string `json:"Locations"`
}

// SourceUserData represents the per-user playback/favorite state on a source item.
type SourceUserData struct {
	Played                bool       `json:"Played"`
	PlayCount             int        `json:"PlayCount"`
	PlaybackPositionTicks int64      `json:"PlaybackPositionTicks"`
	IsFavorite            bool       `json:"IsFavorite"`
	LastPlayedDate        *time.Time `json:"LastPlayedDate,omitempty"`
}

// SourceItem represents a media item returned by GET /Users/{id}/Items.
type SourceItem struct {
	ID                string            `json:"Id"`
	Name              string            `json:"Name"`
	OriginalTitle     string            `json:"OriginalTitle,omitempty"`
	Type              string            `json:"Type"`
	ProductionYear    *int              `json:"ProductionYear,omitempty"`
	SeriesName        string            `json:"SeriesName,omitempty"`
	ParentIndexNumber *int              `json:"ParentIndexNumber,omitempty"`
	IndexNumber       *int              `json:"IndexNumber,omitempty"`
	ProviderIDs       map[string]string `json:"ProviderIds,omitempty"`
	UserData          *SourceUserData   `json:"UserData,omitempty"`
}

// SourceItemsPage wraps a paginated /Users/{id}/Items response.
type SourceItemsPage struct {
	Items            []SourceItem `json:"Items"`
	TotalRecordCount int          `json:"TotalRecordCount"`
}

// ClientOption configures SourceClient behavior.
type ClientOption func(*SourceClient)

// WithHTTPClient sets a custom http.Client.
func WithHTTPClient(c *http.Client) ClientOption {
	return func(sc *SourceClient) {
		if c != nil {
			sc.httpClient = c
		}
	}
}

// WithMaxRetries sets the maximum number of retry attempts on 429/503 responses.
func WithMaxRetries(n int) ClientOption {
	return func(sc *SourceClient) {
		if n >= 0 {
			sc.maxRetries = n
		}
	}
}

// WithBaseBackoff sets the initial exponential backoff delay.
func WithBaseBackoff(d time.Duration) ClientOption {
	return func(sc *SourceClient) {
		if d > 0 {
			sc.baseBackoff = d
		}
	}
}

// WithSleepFunc overrides the sleep implementation used between retries (useful in tests).
func WithSleepFunc(fn func(ctx context.Context, d time.Duration) error) ClientOption {
	return func(sc *SourceClient) {
		if fn != nil {
			sc.sleepFn = fn
		}
	}
}

// SourceClient communicates with a legacy Emby server over HTTP with automatic
// exponential backoff retry on rate-limit / transient availability responses (429/503).
type SourceClient struct {
	baseURL     string
	apiKey      string
	httpClient  *http.Client
	maxRetries  int
	baseBackoff time.Duration
	sleepFn     func(ctx context.Context, d time.Duration) error
}

// NewSourceClient validates the source Emby URL and API key and constructs a SourceClient.
func NewSourceClient(baseURL, apiKey string, opts ...ClientOption) (*SourceClient, error) {
	baseURL = strings.TrimSpace(baseURL)
	apiKey = strings.TrimSpace(apiKey)
	if baseURL == "" {
		return nil, errors.New("source server URL is required")
	}
	if apiKey == "" {
		return nil, errors.New("source API key is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("invalid source server URL: %q", baseURL)
	}
	normalized := strings.TrimRight(parsed.String(), "/")

	sc := &SourceClient{
		baseURL:     normalized,
		apiKey:      apiKey,
		httpClient:  &http.Client{Timeout: defaultHTTPTimeout},
		maxRetries:  defaultMaxRetries,
		baseBackoff: defaultBaseBackoff,
		sleepFn:     defaultSleep,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(sc)
		}
	}
	return sc, nil
}

func defaultSleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// BaseURL returns the normalized base URL of the source Emby server.
func (c *SourceClient) BaseURL() string {
	return c.baseURL
}

// TestConnection queries GET /System/Info on the source Emby server.
func (c *SourceClient) TestConnection(ctx context.Context) (*SourceServerInfo, error) {
	var info SourceServerInfo
	if err := c.doJSON(ctx, "/System/Info", nil, &info); err != nil {
		return nil, err
	}
	info.Status = "ok"
	return &info, nil
}

// ListUsers fetches all users from GET /Users on the source Emby server.
func (c *SourceClient) ListUsers(ctx context.Context) ([]SourceUser, error) {
	var users []SourceUser
	if err := c.doJSON(ctx, "/Users", nil, &users); err != nil {
		return nil, err
	}
	if users == nil {
		users = []SourceUser{}
	}
	return users, nil
}

// ListVirtualFolders fetches all virtual folders/libraries from GET /Library/VirtualFolders.
func (c *SourceClient) ListVirtualFolders(ctx context.Context) ([]SourceVirtualFolder, error) {
	var folders []SourceVirtualFolder
	if err := c.doJSON(ctx, "/Library/VirtualFolders", nil, &folders); err != nil {
		return nil, err
	}
	if folders == nil {
		folders = []SourceVirtualFolder{}
	}
	return folders, nil
}

// FetchUserItemsPage fetches a page of media items and user playback/favorite data for a user.
func (c *SourceClient) FetchUserItemsPage(ctx context.Context, userID string, startIndex, limit int) (*SourceItemsPage, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, errors.New("source user ID is required")
	}
	if startIndex < 0 {
		startIndex = 0
	}
	if limit <= 0 {
		limit = 200
	}

	q := url.Values{}
	q.Set("Recursive", "true")
	q.Set("IncludeItemTypes", "Movie,Series,Episode")
	q.Set("Fields", "ProviderIds,ProductionYear,OriginalTitle,UserData")
	q.Set("EnableUserData", "true")
	q.Set("StartIndex", strconv.Itoa(startIndex))
	q.Set("Limit", strconv.Itoa(limit))

	path := fmt.Sprintf("/Users/%s/Items", url.PathEscape(userID))
	var page SourceItemsPage
	if err := c.doJSON(ctx, path, q, &page); err != nil {
		return nil, err
	}
	if page.Items == nil {
		page.Items = []SourceItem{}
	}
	return &page, nil
}

func (c *SourceClient) doJSON(ctx context.Context, path string, query url.Values, out any) error {
	fullURL := c.baseURL + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("X-Emby-Token", c.apiKey)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = fmt.Errorf("request %s: %w", path, err)
			if attempt < c.maxRetries {
				if sleepErr := c.sleepFn(ctx, c.backoffDelay(attempt, "")); sleepErr != nil {
					return sleepErr
				}
				continue
			}
			return lastErr
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read response %s: %w", path, readErr)
			if attempt < c.maxRetries {
				if sleepErr := c.sleepFn(ctx, c.backoffDelay(attempt, "")); sleepErr != nil {
					return sleepErr
				}
				continue
			}
			return lastErr
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			lastErr = &APIError{
				StatusCode: resp.StatusCode,
				Message:    strings.TrimSpace(string(body)),
			}
			if attempt < c.maxRetries {
				delay := c.backoffDelay(attempt, resp.Header.Get("Retry-After"))
				if sleepErr := c.sleepFn(ctx, delay); sleepErr != nil {
					return sleepErr
				}
				continue
			}
			return lastErr
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return &APIError{
				StatusCode: resp.StatusCode,
				Message:    strings.TrimSpace(string(body)),
			}
		}

		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decode %s response: %w", path, err)
		}
		return nil
	}

	return lastErr
}

func (c *SourceClient) backoffDelay(attempt int, retryAfter string) time.Duration {
	delay := c.baseBackoff << attempt
	if delay > maxBackoffDelay {
		delay = maxBackoffDelay
	}
	if retryAfter != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && secs > 0 {
			ra := time.Duration(secs) * time.Second
			if ra > maxBackoffDelay {
				ra = maxBackoffDelay
			}
			if ra > delay {
				delay = ra
			}
		}
	}
	return delay
}
