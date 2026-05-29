package onedrive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/video-site/backend/internal/drives"
)

const (
	maxSmallUploadSize   = 250 * 1024 * 1024
	defaultRenewAPIURL   = "https://api.oplist.org/onedrive/renewapi"
	onedriveListCooldown = 5 * time.Minute
	onedriveListInterval = 1 * time.Second
)

type Driver struct {
	id            string
	rootID        string
	region        string
	accessToken   string
	refreshToken  string
	isSharePoint  bool
	siteID        string
	apiBaseURL    string
	renewAPIURL   string
	client        *resty.Client
	onTokenUpdate func(access, refresh string)

	listMu       sync.Mutex
	lastListAt   time.Time
	listInterval time.Duration
	listCooldown time.Duration
}

type Config struct {
	ID            string
	RootID        string
	Region        string
	AccessToken   string
	RefreshToken  string
	IsSharePoint  bool
	SiteID        string
	OnTokenUpdate func(access, refresh string)

	RenewAPIURL string
	APIBaseURL  string
}

func New(c Config) *Driver {
	rootID := strings.TrimSpace(c.RootID)
	if rootID == "" {
		rootID = "root"
	}
	region := strings.ToLower(strings.TrimSpace(c.Region))
	if region == "" {
		region = "global"
	}
	h, ok := hostMap[region]
	if !ok {
		h = hostMap["global"]
	}
	apiBaseURL := strings.TrimRight(strings.TrimSpace(c.APIBaseURL), "/")
	if apiBaseURL == "" {
		apiBaseURL = h.api
	}
	renewAPIURL := strings.TrimSpace(c.RenewAPIURL)
	if renewAPIURL == "" {
		renewAPIURL = defaultRenewAPIURL
	}
	return &Driver{
		id:            c.ID,
		rootID:        rootID,
		region:        region,
		accessToken:   strings.TrimSpace(c.AccessToken),
		refreshToken:  strings.TrimSpace(c.RefreshToken),
		isSharePoint:  c.IsSharePoint,
		siteID:        strings.TrimSpace(c.SiteID),
		apiBaseURL:    apiBaseURL,
		renewAPIURL:   renewAPIURL,
		onTokenUpdate: c.OnTokenUpdate,
		client: resty.New().
			SetTimeout(30*time.Second).
			SetHeader("Accept", "application/json, text/plain, */*"),
		listInterval: onedriveListInterval,
		listCooldown: onedriveListCooldown,
	}
}

func (d *Driver) Kind() string   { return "onedrive" }
func (d *Driver) ID() string     { return d.id }
func (d *Driver) RootID() string { return d.rootID }

func (d *Driver) Init(ctx context.Context) error {
	if d.refreshToken == "" {
		return errors.New("onedrive init: refresh_token is required")
	}
	if d.isSharePoint && d.siteID == "" {
		return errors.New("onedrive init: site_id is required for SharePoint")
	}
	return d.refresh(ctx)
}

func (d *Driver) List(ctx context.Context, dirID string) ([]drives.Entry, error) {
	if dirID == "" {
		dirID = d.rootID
	}
	d.listMu.Lock()
	defer d.listMu.Unlock()

	nextLink := d.childrenURL(dirID)
	first := true
	out := make([]drives.Entry, 0)
	for nextLink != "" {
		if err := d.waitForListSlotLocked(ctx); err != nil {
			return nil, err
		}
		var resp filesResp
		err := d.request(ctx, nextLink, http.MethodGet, func(req *resty.Request) {
			if first {
				req.SetQueryParams(map[string]string{
					"$top":    "1000",
					"$select": "id,name,size,fileSystemInfo,content.downloadUrl,file,parentReference,folder",
				})
			}
		}, &resp)
		if err != nil {
			if wait, ok := drives.RateLimitRetryAfter(err); ok {
				if wait <= 0 {
					wait = d.listCooldown
					if wait <= 0 {
						wait = onedriveListCooldown
					}
				}
				log.Printf("[onedrive] list cooling down drive=%s dir=%s cooldown=%s err=%v", d.id, dirID, wait, err)
				if err := sleepContext(ctx, wait); err != nil {
					return nil, err
				}
				continue
			}
			return nil, fmt.Errorf("onedrive list: %w", err)
		}
		for _, item := range resp.Value {
			out = append(out, itemToEntry(item, dirID))
		}
		nextLink = resp.NextLink
		first = false
	}
	return out, nil
}

func (d *Driver) waitForListSlotLocked(ctx context.Context) error {
	if d.listInterval <= 0 || d.lastListAt.IsZero() {
		d.lastListAt = time.Now()
		return ctx.Err()
	}
	next := d.lastListAt.Add(d.listInterval)
	now := time.Now()
	if now.Before(next) {
		if err := sleepContext(ctx, next.Sub(now)); err != nil {
			return err
		}
	}
	d.lastListAt = time.Now()
	return ctx.Err()
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (d *Driver) Stat(ctx context.Context, fileID string) (*drives.Entry, error) {
	var item graphItem
	if err := d.request(ctx, d.itemURL(fileID), http.MethodGet, nil, &item); err != nil {
		return nil, fmt.Errorf("onedrive stat: %w", err)
	}
	e := itemToEntry(item, "")
	return &e, nil
}

func (d *Driver) StreamURL(ctx context.Context, fileID string) (*drives.StreamLink, error) {
	var item graphItem
	if err := d.request(ctx, d.itemURL(fileID), http.MethodGet, nil, &item); err != nil {
		return nil, fmt.Errorf("onedrive download url: %w", err)
	}
	if item.DownloadURL == "" {
		return nil, errors.New("onedrive download url: empty")
	}
	return &drives.StreamLink{
		URL:     item.DownloadURL,
		Headers: http.Header{},
		Expires: time.Now().Add(10 * time.Minute),
	}, nil
}

func (d *Driver) Upload(ctx context.Context, parentID, name string, r io.Reader, size int64) (string, error) {
	if parentID == "" {
		parentID = d.rootID
	}
	if size > maxSmallUploadSize {
		return "", fmt.Errorf("onedrive upload: files over %d bytes require upload session", maxSmallUploadSize)
	}
	data, err := readSmallUpload(r)
	if err != nil {
		return "", err
	}
	u := fmt.Sprintf("%s/items/%s:/%s:/content", d.driveBaseURL(), url.PathEscape(parentID), url.PathEscape(name))
	var item graphItem
	err = d.request(ctx, u, http.MethodPut, func(req *resty.Request) {
		req.SetBody(bytes.NewReader(data))
		req.SetContentLength(true)
	}, &item)
	if err != nil {
		return "", fmt.Errorf("onedrive upload: %w", err)
	}
	if item.ID == "" {
		return "", errors.New("onedrive upload: empty item id")
	}
	return item.ID, nil
}

func readSmallUpload(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, errors.New("onedrive upload: body is required")
	}
	data, err := io.ReadAll(io.LimitReader(r, maxSmallUploadSize+1))
	if err != nil {
		return nil, fmt.Errorf("onedrive upload: read body: %w", err)
	}
	if len(data) > maxSmallUploadSize {
		return nil, fmt.Errorf("onedrive upload: files over %d bytes require upload session", maxSmallUploadSize)
	}
	return data, nil
}

func (d *Driver) EnsureDir(ctx context.Context, pathFromRoot string) (string, error) {
	currentID := d.rootID
	for _, name := range splitPath(pathFromRoot) {
		childID, err := d.findChildDir(ctx, currentID, name)
		if err != nil {
			return "", err
		}
		if childID == "" {
			childID, err = d.makeDir(ctx, currentID, name)
			if err != nil {
				return "", err
			}
		}
		currentID = childID
	}
	return currentID, nil
}

func (d *Driver) findChildDir(ctx context.Context, parentID, name string) (string, error) {
	entries, err := d.List(ctx, parentID)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir && e.Name == name {
			return e.ID, nil
		}
	}
	return "", nil
}

func (d *Driver) makeDir(ctx context.Context, parentID, name string) (string, error) {
	body := map[string]any{
		"name":                              name,
		"folder":                            map[string]any{},
		"@microsoft.graph.conflictBehavior": "rename",
	}
	var item graphItem
	err := d.request(ctx, d.childrenURL(parentID), http.MethodPost, func(req *resty.Request) {
		req.SetBody(body)
	}, &item)
	if err != nil {
		return "", fmt.Errorf("onedrive mkdir %s: %w", name, err)
	}
	if item.ID == "" {
		return "", fmt.Errorf("onedrive mkdir %s: empty item id", name)
	}
	return item.ID, nil
}

func (d *Driver) request(ctx context.Context, rawURL, method string, configure func(*resty.Request), out any) error {
	return d.requestOnce(ctx, rawURL, method, configure, out, true)
}

func (d *Driver) requestOnce(ctx context.Context, rawURL, method string, configure func(*resty.Request), out any, retry bool) error {
	req := d.client.R().
		SetContext(ctx).
		SetHeader("Authorization", "Bearer "+d.accessToken)
	if configure != nil {
		configure(req)
	}
	if out != nil {
		req.SetResult(out)
	}
	var graphErr graphErrorResp
	req.SetError(&graphErr)
	res, err := req.Execute(method, rawURL)
	if err != nil {
		return err
	}
	if isRateLimitResponse(res, graphErr.Error.Code, graphErr.Error.Message) {
		return onedriveRateLimitError(res, graphErr.Error.Message)
	}
	if graphErr.Error.Code != "" {
		if graphErr.Error.Code == "InvalidAuthenticationToken" && retry {
			if err := d.refresh(ctx); err != nil {
				return err
			}
			return d.requestOnce(ctx, rawURL, method, configure, out, false)
		}
		if graphErr.Error.Message != "" {
			return errors.New(graphErr.Error.Message)
		}
		return fmt.Errorf("graph api error: %s", graphErr.Error.Code)
	}
	if res.IsError() {
		return fmt.Errorf("graph api error: status=%d body=%s", res.StatusCode(), strings.TrimSpace(res.String()))
	}
	return nil
}

func (d *Driver) refresh(ctx context.Context) error {
	var out tokenResp
	res, err := d.client.R().
		SetContext(ctx).
		SetQueryParams(map[string]string{
			"refresh_ui": d.refreshToken,
			"server_use": "true",
			"driver_txt": "onedrive_pr",
		}).
		SetResult(&out).
		SetError(&out).
		Get(d.renewAPIURL)
	if err != nil {
		return fmt.Errorf("onedrive refresh token: %w", err)
	}
	if res.StatusCode() == http.StatusTooManyRequests {
		return onedriveRateLimitError(res, "token refresh throttled")
	}
	if out.Text != "" {
		return fmt.Errorf("onedrive refresh token: %s", out.Text)
	}
	if out.Error != "" {
		if out.Description != "" {
			return fmt.Errorf("onedrive refresh token: %s", out.Description)
		}
		return fmt.Errorf("onedrive refresh token: %s", out.Error)
	}
	if res.IsError() {
		return fmt.Errorf("onedrive refresh token: status=%d body=%s", res.StatusCode(), strings.TrimSpace(res.String()))
	}
	if out.AccessToken == "" || out.RefreshToken == "" {
		return errors.New("onedrive refresh token: empty token")
	}
	d.accessToken = out.AccessToken
	d.refreshToken = out.RefreshToken
	if d.onTokenUpdate != nil {
		d.onTokenUpdate(out.AccessToken, out.RefreshToken)
	}
	return nil
}

func isRateLimitResponse(res *resty.Response, code, message string) bool {
	if isRateLimitCode(code) || isRateLimitMessage(message) {
		return true
	}
	if res == nil {
		return false
	}
	if res.StatusCode() == http.StatusTooManyRequests {
		return true
	}
	if res.Header().Get("Retry-After") == "" {
		return false
	}
	switch res.StatusCode() {
	case http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func isRateLimitCode(code string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(code), "_", ""))
	normalized = strings.ReplaceAll(normalized, "-", "")
	switch normalized {
	case "toomanyrequests",
		"activitylimitreached",
		"throttledrequest",
		"requestthrottled",
		"resourcethrottled",
		"applicationthrottled",
		"tenantthrottled":
		return true
	default:
		return false
	}
}

func isRateLimitMessage(message string) bool {
	text := strings.ToLower(strings.TrimSpace(message))
	if text == "" {
		return false
	}
	return strings.Contains(text, "too many requests") ||
		strings.Contains(text, "throttl") ||
		strings.Contains(text, "rate limit") ||
		strings.Contains(text, "activity limit") ||
		strings.Contains(text, "temporarily blocked")
}

func onedriveRateLimitError(res *resty.Response, message string) error {
	if strings.TrimSpace(message) == "" {
		message = "onedrive rate limited"
	}
	if res != nil && strings.TrimSpace(res.String()) != "" {
		message = fmt.Sprintf("%s: status=%d body=%s", message, res.StatusCode(), strings.TrimSpace(res.String()))
	}
	return &drives.RateLimitError{
		Provider:   "onedrive",
		RetryAfter: parseRetryAfter(res),
		Err:        errors.New(message),
	}
}

func parseRetryAfter(res *resty.Response) time.Duration {
	if res == nil {
		return 0
	}
	raw := strings.TrimSpace(res.Header().Get("Retry-After"))
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(raw); err == nil {
		d := time.Until(when)
		if d > 0 {
			return d
		}
	}
	return 0
}

func (d *Driver) driveBaseURL() string {
	if d.isSharePoint {
		return fmt.Sprintf("%s/v1.0/sites/%s/drive", d.apiBaseURL, url.PathEscape(d.siteID))
	}
	return d.apiBaseURL + "/v1.0/me/drive"
}

func (d *Driver) itemURL(itemID string) string {
	if itemID == "" {
		itemID = d.rootID
	}
	return d.driveBaseURL() + "/items/" + url.PathEscape(itemID)
}

func (d *Driver) childrenURL(parentID string) string {
	return d.itemURL(parentID) + "/children"
}

func itemToEntry(item graphItem, fallbackParentID string) drives.Entry {
	parentID := item.ParentRef.ID
	if parentID == "" {
		parentID = fallbackParentID
	}
	isDir := item.Folder != nil
	mod := time.Time{}
	if item.FileSystemInfo != nil {
		mod = item.FileSystemInfo.LastModifiedDateTime
	}
	mimeType := ""
	if item.File != nil {
		mimeType = item.File.MimeType
	}
	if mimeType == "" && !isDir {
		mimeType = guessMime(item.Name)
	}
	return drives.Entry{
		ID:       item.ID,
		Name:     item.Name,
		Size:     item.Size,
		IsDir:    isDir,
		ParentID: parentID,
		MimeType: mimeType,
		ModTime:  mod,
	}
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func guessMime(name string) string {
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".mp4":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	}
	return "application/octet-stream"
}

var _ drives.Drive = (*Driver)(nil)
