package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	ErrObjectNotFound = errors.New("storage object not found")
	ErrSizeMismatch   = errors.New("storage object size mismatch")
)

type StorageClient interface {
	CreateSignedUploadURL(ctx context.Context, bucket, objectKey string) (string, error)
	VerifyObject(ctx context.Context, bucket, objectKey string, expectedSize int64) error
	DeleteObjects(ctx context.Context, bucket string, objectKeys []string) error
}

type Client struct {
	supabaseURL    string
	serviceRoleKey string
	httpClient     *http.Client
}

func NewClient(supabaseURL, serviceRoleKey string) *Client {
	return &Client{
		supabaseURL:    strings.TrimRight(supabaseURL, "/"),
		serviceRoleKey: serviceRoleKey,
		httpClient:     &http.Client{Timeout: 15 * time.Second},
	}
}

type signResponse struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

type objectInfo struct {
	Size int64 `json:"size"`
}

func (c *Client) CreateSignedUploadURL(ctx context.Context, bucket, objectKey string) (string, error) {
	endpoint := fmt.Sprintf("%s/storage/v1/object/upload/sign/%s/%s", c.supabaseURL, bucket, objectKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", fmt.Errorf("failed to create upload sign request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.serviceRoleKey)
	req.Header.Set("apikey", c.serviceRoleKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute upload sign request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("supabase sign upload returned %d: %s", resp.StatusCode, string(body))
	}

	var res signResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", fmt.Errorf("failed to decode upload sign response: %w", err)
	}

	if strings.HasPrefix(res.URL, "http://") || strings.HasPrefix(res.URL, "https://") {
		return res.URL, nil
	}
	if strings.HasPrefix(res.URL, "/storage/v1") {
		return c.supabaseURL + res.URL, nil
	}
	if strings.HasPrefix(res.URL, "/") {
		return c.supabaseURL + "/storage/v1" + res.URL, nil
	}
	return c.supabaseURL + "/storage/v1/" + res.URL, nil
}

func (c *Client) VerifyObject(ctx context.Context, bucket, objectKey string, expectedSize int64) error {
	endpoint := fmt.Sprintf("%s/storage/v1/object/info/%s/%s", c.supabaseURL, bucket, objectKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create object info request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.serviceRoleKey)
	req.Header.Set("apikey", c.serviceRoleKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute object info request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrObjectNotFound
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supabase object info returned %d: %s", resp.StatusCode, string(body))
	}

	var info objectInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return fmt.Errorf("failed to decode object info: %w", err)
	}

	if info.Size != expectedSize {
		return fmt.Errorf("%w: expected %d, got %d", ErrSizeMismatch, expectedSize, info.Size)
	}

	return nil
}

func (c *Client) DeleteObjects(ctx context.Context, bucket string, objectKeys []string) error {
	if len(objectKeys) == 0 {
		return nil
	}

	endpoint := fmt.Sprintf("%s/storage/v1/object/%s", c.supabaseURL, bucket)
	payload, err := json.Marshal(map[string][]string{
		"prefixes": objectKeys,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal delete payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create delete request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.serviceRoleKey)
	req.Header.Set("apikey", c.serviceRoleKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute delete request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("supabase delete objects returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
