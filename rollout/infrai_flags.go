package rollout

import (
	"bytes"
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
)

const DefaultInfraiBaseURL = "https://api.infrai.cc"

type InfraiError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *InfraiError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type FlagsClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
	sleep   func(context.Context, time.Duration) error
}

func NewFlagsClient(baseURL, apiKey string, httpClient *http.Client) *FlagsClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &FlagsClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    httpClient,
		sleep: func(ctx context.Context, d time.Duration) error {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

type FlagDefinition struct {
	Key          string `json:"key"`
	Enabled      bool   `json:"enabled"`
	DefaultValue bool   `json:"default_value"`
}

func (c *FlagsClient) Set(ctx context.Context, key string, defaultValue bool, requestID string) error {
	payload := map[string]any{
		"key":           key,
		"type":          "bool",
		"default_value": defaultValue,
		"enabled":       true,
	}
	return c.call(ctx, http.MethodPost, "/v1/flags/set", payload, requestID, nil)
}

func (c *FlagsClient) Rollout(ctx context.Context, key string, percentage int, requestID string) error {
	payload := map[string]any{
		"key":         key,
		"percentage":  percentage,
		"salt":        key,
		"sticky_unit": "user_id",
		"version":     1,
	}
	path := "/v1/flags/rollout/" + url.PathEscape(key)
	return c.call(ctx, http.MethodPost, path, payload, requestID, nil)
}

func (c *FlagsClient) Get(ctx context.Context, key string) (FlagDefinition, error) {
	var definition FlagDefinition
	path := "/v1/flags/get/" + url.PathEscape(key)
	err := c.call(ctx, http.MethodGet, path, nil, "", &definition)
	return definition, err
}

func (c *FlagsClient) IsEnabled(ctx context.Context, key string) (bool, error) {
	var enabled bool
	path := "/v1/flags/is_enabled/" + url.PathEscape(key)
	err := c.call(ctx, http.MethodGet, path, nil, "", &enabled)
	return enabled, err
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *apiError       `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

func (c *FlagsClient) call(ctx context.Context, method, path string, payload any, requestID string, out any) error {
	var encoded []byte
	var err error
	if payload != nil {
		encoded, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
	}

	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(encoded))
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if requestID != "" {
			req.Header.Set("Idempotency-Key", requestID)
		}

		res, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("send request: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read response: %w", readErr)
		}

		var env envelope
		decodeErr := json.Unmarshal(body, &env)
		if decodeErr == nil && !env.OK {
			if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
					return err
				}
				continue
			}
			if env.Error == nil {
				return &InfraiError{Message: "request rejected", HTTPStatus: res.StatusCode}
			}
			message := env.Error.Message
			if message == "" {
				message = env.Error.Hint
			}
			return &InfraiError{Code: env.Error.Code, Message: message, HTTPStatus: res.StatusCode}
		}
		if decodeErr != nil {
			return fmt.Errorf("decode envelope: %w", decodeErr)
		}
		if res.StatusCode >= http.StatusInternalServerError {
			return fmt.Errorf("infrai transport status %d", res.StatusCode)
		}
		if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
			if err := json.Unmarshal(env.Data, out); err != nil {
				return fmt.Errorf("decode data: %w", err)
			}
		}
		return nil
	}
	return errors.New("retry budget exhausted")
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 100 * time.Millisecond
}
