// Package client provides a thin HTTP client for the WireLog API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
)

const (
	maxRetries     = 3
	baseRetryDelay = 500 * time.Millisecond
)

// Client is a synchronous HTTP client for the WireLog API.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	UserAgent  string
}

// New creates a new Client.
func New(baseURL, apiKey, version string, timeout time.Duration) *Client {
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTPClient: &http.Client{
			Timeout: timeout,
		},
		UserAgent: "wl/" + version,
	}
}

// --- Request/Response types ---

// TrackEvent is a single event for the track endpoint.
type TrackEvent struct {
	EventType       string         `json:"event_type"`
	UserID          string         `json:"user_id,omitempty"`
	DeviceID        string         `json:"device_id,omitempty"`
	SessionID       string         `json:"session_id,omitempty"`
	Time            string         `json:"time,omitempty"`
	EventProperties map[string]any `json:"event_properties,omitempty"`
	UserProperties  map[string]any `json:"user_properties,omitempty"`
	InsertID        string         `json:"insert_id,omitempty"`
	Origin          string         `json:"origin,omitempty"`
}

// TrackRequest is the request body for POST /track.
type TrackRequest struct {
	Events []TrackEvent `json:"events"`
}

// TrackResponse is the response from POST /track.
type TrackResponse struct {
	Accepted int `json:"accepted"`
}

// IdentifyParams is the request body for POST /identify.
type IdentifyParams struct {
	UserID          string           `json:"user_id"`
	DeviceID        string           `json:"device_id,omitempty"`
	UserProperties  map[string]any   `json:"user_properties,omitempty"`
	UserPropertyOps *UserPropertyOps `json:"user_property_ops,omitempty"`
}

// UserPropertyOps holds structured property operations for identify.
type UserPropertyOps struct {
	Set     map[string]any     `json:"$set,omitempty"`
	SetOnce map[string]any     `json:"$set_once,omitempty"`
	Add     map[string]float64 `json:"$add,omitempty"`
	Unset   []string           `json:"$unset,omitempty"`
}

// IdentifyResponse is the response from POST /identify.
type IdentifyResponse struct {
	OK bool `json:"ok"`
}

// Project represents a project from the admin API.
type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	PublicKey string `json:"public_key,omitempty"`
	SecretKey string `json:"secret_key,omitempty"`
	CreatedAt string `json:"created_at"`
}

// Org represents an organization.
type Org struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug,omitempty"`
	Plan     string `json:"plan,omitempty"`
	AdminKey string `json:"admin_key,omitempty"`
}

// UsageRow is a single day of usage data.
type UsageRow struct {
	Date           string `json:"date"`
	EventsIngested int64  `json:"events_ingested"`
	EventsStored   int64  `json:"events_stored"`
	QueriesRun     int64  `json:"queries_run"`
}

// HealthStatus holds the result of health checks.
type HealthStatus struct {
	Healthy bool   `json:"healthy"`
	Health  string `json:"health"`
	Ready   string `json:"ready"`
}

// QueryJSONResponse is the structured response returned by /query in JSON mode.
type QueryJSONResponse struct {
	Columns     []string         `json:"columns"`
	Rows        []map[string]any `json:"rows"`
	Total       int              `json:"total"`
	ElapsedMS   int64            `json:"elapsed_ms"`
	RowsScanned int64            `json:"rows_scanned"`
	Period      string           `json:"period"`
	Mode        string           `json:"mode"`
	Metadata    map[string]any   `json:"metadata,omitempty"`
}

// --- API methods ---

// Query sends a DSL query and returns the raw response body.
func (c *Client) Query(ctx context.Context, q, format string, limit, offset int) ([]byte, string, error) {
	body := map[string]any{"q": q}
	if format != "" {
		body["format"] = format
	}
	if limit > 0 {
		body["limit"] = limit
	}
	if offset > 0 {
		body["offset"] = offset
	}

	resp, err := c.doWithRetry(ctx, http.MethodPost, "/query", body)
	if err != nil {
		return nil, "", err
	}
	defer closeBody(resp)

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read response: %w", err)
	}

	ct := resp.Header.Get("Content-Type")
	return data, ct, nil
}

// QueryJSON sends a DSL query and decodes the JSON response.
func (c *Client) QueryJSON(ctx context.Context, q string, limit, offset int) (*QueryJSONResponse, error) {
	body := map[string]any{
		"q":      q,
		"format": "json",
	}
	if limit > 0 {
		body["limit"] = limit
	}
	if offset > 0 {
		body["offset"] = offset
	}

	resp, err := c.doWithRetry(ctx, http.MethodPost, "/query", body)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)

	var result QueryJSONResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return nil, fmt.Errorf("decode query response: %w", err)
	}
	return &result, nil
}

// Track sends one or more events. Auto-generates insert_id when missing.
func (c *Client) Track(ctx context.Context, events []TrackEvent) (*TrackResponse, error) {
	for i := range events {
		if events[i].InsertID == "" {
			events[i].InsertID = uuid.New().String()
		}
	}

	req := TrackRequest{Events: events}

	resp, err := c.doWithRetry(ctx, http.MethodPost, "/track", req)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)

	var result TrackResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// Identify sends an identify call.
func (c *Client) Identify(ctx context.Context, params IdentifyParams) error {
	resp, err := c.doWithRetry(ctx, http.MethodPost, "/identify", params)
	if err != nil {
		return err
	}
	closeBody(resp)
	return nil
}

// ListProjects lists all projects in the org (admin API).
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	resp, err := c.doWithRetry(ctx, http.MethodGet, "/api/admin/projects", nil)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)

	var projects []Project
	err = json.NewDecoder(resp.Body).Decode(&projects)
	if err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return projects, nil
}

// CreateProject creates a new project (admin API).
func (c *Client) CreateProject(ctx context.Context, name string) (*Project, error) {
	resp, err := c.doWithRetry(ctx, http.MethodPost, "/api/admin/projects", map[string]string{"name": name})
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)

	var project Project
	err = json.NewDecoder(resp.Body).Decode(&project)
	if err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &project, nil
}

// GetProject gets a project by ID (admin API).
func (c *Client) GetProject(ctx context.Context, projectID string) (*Project, error) {
	resp, err := c.doWithRetry(ctx, http.MethodGet, "/api/admin/projects/"+projectID, nil)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)

	var project Project
	err = json.NewDecoder(resp.Body).Decode(&project)
	if err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &project, nil
}

// DeleteProject deletes a project (admin API). Requires name confirmation.
func (c *Client) DeleteProject(ctx context.Context, projectID, projectName string) error {
	body := map[string]string{
		"project_name":         projectName,
		"project_name_confirm": projectName,
	}
	resp, err := c.doWithRetry(ctx, http.MethodDelete, "/api/admin/projects/"+projectID, body)
	if err != nil {
		return err
	}
	closeBody(resp)
	return nil
}

// RotateSecretKey rotates the secret key for a project (admin API).
func (c *Client) RotateSecretKey(ctx context.Context, projectID string) (*Project, error) {
	resp, err := c.doWithRetry(ctx, http.MethodPost, "/api/admin/projects/"+projectID+"/rotate-secret-key", nil)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)

	var project Project
	err = json.NewDecoder(resp.Body).Decode(&project)
	if err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &project, nil
}

// GetProjectUsage gets usage stats for a project (admin API).
func (c *Client) GetProjectUsage(ctx context.Context, projectID, from, to string) ([]UsageRow, error) {
	path := "/api/admin/projects/" + projectID + "/usage"
	sep := "?"
	if from != "" {
		path += sep + "from=" + from
		sep = "&"
	}
	if to != "" {
		path += sep + "to=" + to
	}

	resp, err := c.doWithRetry(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)

	var rows []UsageRow
	err = json.NewDecoder(resp.Body).Decode(&rows)
	if err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return rows, nil
}

// GDPRExport streams a user's data as NDJSON (returns the response body reader).
func (c *Client) GDPRExport(ctx context.Context, userID string) (io.ReadCloser, error) {
	resp, err := c.doWithRetry(ctx, http.MethodGet, "/api/gdpr/users/"+userID+"/export", nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// GDPRDelete deletes all data for a user.
func (c *Client) GDPRDelete(ctx context.Context, userID string) error {
	resp, err := c.doWithRetry(ctx, http.MethodDelete, "/api/gdpr/users/"+userID, nil)
	if err != nil {
		return err
	}
	closeBody(resp)
	return nil
}

// Health checks both /healthz and /readyz.
func (c *Client) Health(ctx context.Context) (*HealthStatus, error) {
	status := &HealthStatus{}

	healthResp, err := c.doRaw(ctx, http.MethodGet, "/healthz", nil)
	if err != nil {
		status.Health = "unreachable"
		return status, nil //nolint:nilerr // partial status is valid
	}
	healthBody, _ := io.ReadAll(healthResp.Body)
	closeBody(healthResp)
	status.Health = string(healthBody)

	readyResp, err := c.doRaw(ctx, http.MethodGet, "/readyz", nil)
	if err != nil {
		status.Ready = "unreachable"
		return status, nil //nolint:nilerr // partial status is valid
	}
	readyBody, _ := io.ReadAll(readyResp.Body)
	closeBody(readyResp)
	status.Ready = string(readyBody)

	status.Healthy = status.Health == "ok" && status.Ready == "ready"
	return status, nil
}

// --- HTTP transport ---

func (c *Client) doWithRetry(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var lastErr error
	for attempt := range maxRetries {
		resp, err := c.doRaw(ctx, method, path, body)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode < 400 {
			return resp, nil
		}

		respBody, _ := io.ReadAll(resp.Body)
		closeBody(resp)

		apiErr := &APIError{
			StatusCode: resp.StatusCode,
			RetryAfter: resp.Header.Get("Retry-After"),
		}

		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != "" {
			apiErr.Message = errResp.Error
		} else {
			apiErr.Message = string(respBody)
		}

		// Retry on 429 with backoff
		if resp.StatusCode == 429 && attempt < maxRetries-1 {
			delay := retryDelay(attempt, apiErr.RetryAfter)
			lastErr = apiErr
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
				continue
			}
		}

		return nil, apiErr
	}
	return nil, lastErr
}

func (c *Client) doRaw(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.APIKey != "" {
		req.Header.Set("X-API-Key", c.APIKey)
	}
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	return resp, nil
}

func closeBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

func retryDelay(attempt int, retryAfter string) time.Duration {
	if retryAfter != "" {
		if secs, err := strconv.Atoi(retryAfter); err == nil {
			return time.Duration(secs) * time.Second
		}
	}
	return baseRetryDelay * time.Duration(math.Pow(2, float64(attempt)))
}
