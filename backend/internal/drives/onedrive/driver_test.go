package onedrive

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/video-site/backend/internal/drives"
)

func TestInitRefreshesTokenThroughOpenListOnlineAPIAndPersistsUpdate(t *testing.T) {
	var tokenRequestSeen bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/renewapi" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		tokenRequestSeen = true
		want := map[string]string{
			"refresh_ui": "old-refresh",
			"server_use": "true",
			"driver_txt": "onedrive_pr",
		}
		for key, value := range want {
			if got := r.URL.Query().Get(key); got != value {
				t.Fatalf("query %s = %q, want %q", key, got, value)
			}
		}
		writeJSON(t, w, map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	var persistedAccess, persistedRefresh string
	d := New(Config{
		ID:           "od-main",
		RefreshToken: "old-refresh",
		RenewAPIURL:  srv.URL + "/renewapi",
		APIBaseURL:   srv.URL,
		OnTokenUpdate: func(access, refresh string) {
			persistedAccess = access
			persistedRefresh = refresh
		},
	})

	if d.Kind() != "onedrive" {
		t.Fatalf("kind = %q, want onedrive", d.Kind())
	}
	if d.ID() != "od-main" {
		t.Fatalf("id = %q, want od-main", d.ID())
	}
	if d.RootID() != "root" {
		t.Fatalf("root id = %q, want root", d.RootID())
	}
	if err := d.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	if !tokenRequestSeen {
		t.Fatal("OpenList renew API was not called")
	}
	if persistedAccess != "new-access" || persistedRefresh != "new-refresh" {
		t.Fatalf("persisted tokens = %q/%q, want new-access/new-refresh", persistedAccess, persistedRefresh)
	}
}

func TestListFollowsPaginationAndMapsEntries(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q, want bearer token", got)
		}
		switch r.URL.Path {
		case "/v1.0/me/drive/items/root/children":
			if r.URL.Query().Get("$top") != "1000" {
				t.Fatalf("$top = %q, want 1000", r.URL.Query().Get("$top"))
			}
			writeJSON(t, w, map[string]any{
				"value": []map[string]any{
					{
						"id":   "folder-id",
						"name": "Movies",
						"size": 0,
						"folder": map[string]any{
							"childCount": 2,
						},
						"fileSystemInfo": map[string]any{
							"lastModifiedDateTime": "2026-05-10T12:30:00Z",
						},
						"parentReference": map[string]any{
							"id": "root",
						},
					},
					{
						"id":   "file-id",
						"name": "demo.mp4",
						"size": 12345,
						"file": map[string]any{
							"mimeType": "video/mp4",
						},
						"fileSystemInfo": map[string]any{
							"lastModifiedDateTime": "2026-05-10T13:30:00Z",
						},
						"thumbnails": []map[string]any{
							{"medium": map[string]any{"url": "https://thumb.example/demo.jpg"}},
						},
						"parentReference": map[string]any{
							"id": "root",
						},
					},
				},
				"@odata.nextLink": srv.URL + "/next-page",
			})
		case "/next-page":
			writeJSON(t, w, map[string]any{
				"value": []map[string]any{
					{
						"id":   "file-2",
						"name": "second.mkv",
						"size": 77,
						"file": map[string]any{
							"mimeType": "video/x-matroska",
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	d := New(Config{
		ID:           "od-main",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		APIBaseURL:   srv.URL,
	})

	got, err := d.List(context.Background(), "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("entries len = %d, want 3", len(got))
	}
	if !got[0].IsDir || got[0].ID != "folder-id" || got[0].ParentID != "root" {
		t.Fatalf("folder entry = %#v", got[0])
	}
	if got[1].IsDir || got[1].MimeType != "video/mp4" || got[1].ThumbnailURL != "" {
		t.Fatalf("file entry = %#v", got[1])
	}
	if got[1].ModTime.IsZero() {
		t.Fatal("file mod time should be parsed")
	}
	if got[2].Name != "second.mkv" || got[2].Size != 77 {
		t.Fatalf("paginated entry = %#v", got[2])
	}
}

func TestGraphItemWithoutFolderFacetIsNotDirectory(t *testing.T) {
	got := itemToEntry(graphItem{
		ID:   "special-id",
		Name: "个人保管库",
	}, "root")

	if got.IsDir {
		t.Fatalf("special Graph item without folder facet should not be treated as a directory: %#v", got)
	}
}

func TestGraph429ReturnsRateLimitErrorWithRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "TooManyRequests",
				"message": "throttled",
			},
		}); err != nil {
			t.Fatalf("write json: %v", err)
		}
	}))
	defer srv.Close()

	d := New(Config{
		ID:           "od-main",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		APIBaseURL:   srv.URL,
	})

	_, err := d.StreamURL(context.Background(), "file-id")
	if err == nil {
		t.Fatal("list succeeded, want rate limit error")
	}
	var rateLimit *drives.RateLimitError
	if !errors.As(err, &rateLimit) {
		t.Fatalf("error = %T %[1]v, want RateLimitError", err)
	}
	if rateLimit.RetryAfter != 2*time.Minute {
		t.Fatalf("retry after = %v, want 2m", rateLimit.RetryAfter)
	}
}

func TestGraphThrottleMessageReturnsRateLimitError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "generalException",
				"message": "The request has been throttled. Please try again later.",
			},
		}); err != nil {
			t.Fatalf("write json: %v", err)
		}
	}))
	defer srv.Close()

	d := New(Config{
		ID:           "od-main",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		APIBaseURL:   srv.URL,
	})

	_, err := d.StreamURL(context.Background(), "file-id")
	if err == nil {
		t.Fatal("list succeeded, want rate limit error")
	}
	var rateLimit *drives.RateLimitError
	if !errors.As(err, &rateLimit) {
		t.Fatalf("error = %T %[1]v, want RateLimitError", err)
	}
}

func TestListCoolsDownAndRetriesOneDriveRateLimit(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.0/me/drive/items/root/children" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		calls++
		if calls == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			if err := json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"code":    "TooManyRequests",
					"message": "throttled",
				},
			}); err != nil {
				t.Fatalf("write json: %v", err)
			}
			return
		}
		writeJSON(t, w, map[string]any{
			"value": []map[string]any{
				{
					"id":   "file-id",
					"name": "demo.mp4",
					"size": 100,
					"file": map[string]any{"mimeType": "video/mp4"},
				},
			},
		})
	}))
	defer srv.Close()

	d := New(Config{
		ID:           "od-main",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		APIBaseURL:   srv.URL,
	})
	d.listInterval = 0
	d.listCooldown = time.Millisecond

	got, err := d.List(context.Background(), "root")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want retry after rate limit", calls)
	}
	if len(got) != 1 || got[0].ID != "file-id" {
		t.Fatalf("entries = %#v, want retried file", got)
	}
}

func TestStatAndStreamURLUseDriveItemMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q, want bearer token", got)
		}
		if r.Method != http.MethodGet || r.URL.Path != "/v1.0/me/drive/items/file-id" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		writeJSON(t, w, map[string]any{
			"id":   "file-id",
			"name": "movie.mov",
			"size": 2048,
			"file": map[string]any{
				"mimeType": "video/quicktime",
			},
			"parentReference": map[string]any{
				"id": "parent-id",
			},
			"@microsoft.graph.downloadUrl": "https://download.example/movie.mov",
		})
	}))
	defer srv.Close()

	d := New(Config{
		ID:           "od-main",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		APIBaseURL:   srv.URL,
	})

	entry, err := d.Stat(context.Background(), "file-id")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if entry.ID != "file-id" || entry.Name != "movie.mov" || entry.ParentID != "parent-id" {
		t.Fatalf("entry = %#v", entry)
	}

	link, err := d.StreamURL(context.Background(), "file-id")
	if err != nil {
		t.Fatalf("stream url: %v", err)
	}
	if link.URL != "https://download.example/movie.mov" {
		t.Fatalf("stream url = %q, want download url", link.URL)
	}
	if len(link.Headers) != 0 {
		t.Fatalf("headers = %#v, want none", link.Headers)
	}
}

func TestEnsureDirCreatesMissingFolders(t *testing.T) {
	var created []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q, want bearer token", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/me/drive/items/root/children":
			writeJSON(t, w, map[string]any{
				"value": []map[string]any{
					{
						"id":     "existing-id",
						"name":   "existing",
						"folder": map[string]any{},
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1.0/me/drive/items/existing-id/children":
			writeJSON(t, w, map[string]any{"value": []map[string]any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1.0/me/drive/items/existing-id/children":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode mkdir body: %v", err)
			}
			created = append(created, body["name"].(string))
			if body["@microsoft.graph.conflictBehavior"] != "rename" {
				t.Fatalf("conflict behavior = %#v, want rename", body["@microsoft.graph.conflictBehavior"])
			}
			writeJSON(t, w, map[string]any{
				"id":     "created-id",
				"name":   body["name"],
				"folder": map[string]any{},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	d := New(Config{
		ID:           "od-main",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		APIBaseURL:   srv.URL,
	})

	got, err := d.EnsureDir(context.Background(), "/existing/previews")
	if err != nil {
		t.Fatalf("ensure dir: %v", err)
	}
	if got != "created-id" {
		t.Fatalf("dir id = %q, want created-id", got)
	}
	if len(created) != 1 || created[0] != "previews" {
		t.Fatalf("created folders = %#v, want previews", created)
	}
}

func TestUploadSmallFileReturnsNewItemID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("authorization = %q, want bearer token", got)
		}
		if r.Method != http.MethodPut || r.URL.EscapedPath() != "/v1.0/me/drive/items/parent-id:/preview%20file.mp4:/content" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upload body: %v", err)
		}
		if string(data) != "preview-bytes" {
			t.Fatalf("upload body = %q, want preview-bytes", string(data))
		}
		writeJSON(t, w, map[string]any{
			"id":   "uploaded-id",
			"name": "preview file.mp4",
		})
	}))
	defer srv.Close()

	d := New(Config{
		ID:           "od-main",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		APIBaseURL:   srv.URL,
	})

	got, err := d.Upload(context.Background(), "parent-id", "preview file.mp4", strings.NewReader("preview-bytes"), int64(len("preview-bytes")))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if got != "uploaded-id" {
		t.Fatalf("uploaded id = %q, want uploaded-id", got)
	}
}

func TestUploadRefreshesExpiredTokenAndReplaysBody(t *testing.T) {
	var uploadAttempts int
	var tokenRefreshes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.EscapedPath() == "/v1.0/me/drive/items/parent-id:/preview.mp4:/content":
			uploadAttempts++
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read upload body: %v", err)
			}
			if string(data) != "preview-bytes" {
				t.Fatalf("upload attempt %d body = %q, want preview-bytes", uploadAttempts, string(data))
			}
			if uploadAttempts == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				if err := json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{
						"code":    "InvalidAuthenticationToken",
						"message": "token expired",
					},
				}); err != nil {
					t.Fatalf("write json: %v", err)
				}
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer new-access" {
				t.Fatalf("retry authorization = %q, want new access token", got)
			}
			writeJSON(t, w, map[string]any{"id": "uploaded-id"})
		case r.Method == http.MethodGet && r.URL.Path == "/renewapi":
			tokenRefreshes++
			if got := r.URL.Query().Get("refresh_ui"); got != "old-refresh" {
				t.Fatalf("refresh_ui = %q, want old-refresh", got)
			}
			writeJSON(t, w, map[string]any{
				"access_token":  "new-access",
				"refresh_token": "new-refresh",
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	d := New(Config{
		ID:           "od-main",
		AccessToken:  "expired-access",
		RefreshToken: "old-refresh",
		RenewAPIURL:  srv.URL + "/renewapi",
		APIBaseURL:   srv.URL,
	})

	got, err := d.Upload(context.Background(), "parent-id", "preview.mp4", strings.NewReader("preview-bytes"), int64(len("preview-bytes")))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if got != "uploaded-id" {
		t.Fatalf("uploaded id = %q, want uploaded-id", got)
	}
	if uploadAttempts != 2 || tokenRefreshes != 1 {
		t.Fatalf("attempts/refreshes = %d/%d, want 2/1", uploadAttempts, tokenRefreshes)
	}
}

func TestSharePointUsesSiteDriveBase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/renewapi":
			if r.Method != http.MethodGet {
				t.Fatalf("renew method = %s, want GET", r.Method)
			}
			writeJSON(t, w, map[string]any{
				"access_token":  "access-token",
				"refresh_token": "refresh-token",
			})
		case "/v1.0/sites/site-123/drive/items/root/children":
			writeJSON(t, w, map[string]any{"value": []map[string]any{}})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer srv.Close()

	d := New(Config{
		ID:           "od-sp",
		RefreshToken: "old-refresh",
		IsSharePoint: true,
		SiteID:       "site-123",
		RenewAPIURL:  srv.URL + "/renewapi",
		APIBaseURL:   srv.URL,
	})
	if err := d.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := d.List(context.Background(), "root"); err != nil {
		t.Fatalf("list: %v", err)
	}
}

func TestDriverImplementsInterface(t *testing.T) {
	var _ drives.Drive = (*Driver)(nil)
}

func writeJSON(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("write json: %v", err)
	}
}
