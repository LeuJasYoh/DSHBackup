package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"DSHBackup/internal/config"
	"DSHBackup/internal/logging"
	"DSHBackup/internal/platform"
)

const (
	DefaultAPIBase    = "https://api.github.com"
	DefaultUploadBase = "https://uploads.github.com"
	deviceCodeURL     = "https://github.com/login/device/code"
	accessTokenURL    = "https://github.com/login/oauth/access_token"
)

type Client struct {
	token      string
	httpClient *http.Client
	logger     *logging.Logger
	APIBase    string
	UploadBase string
	DeviceURL  string
	TokenURL   string
	UserAgent  string
}

type ProxyConfig struct {
	Mode        string
	URL         string
	Username    string
	Password    string
	BypassLocal bool
}

type APIError struct {
	Status     int
	Message    string
	RequestID  string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	message := e.Message
	if e.RequestID != "" {
		message += "（请求 ID " + e.RequestID + "）"
	}
	return message
}

func New(token string, proxy ProxyConfig, logger *logging.Logger) (*Client, error) {
	httpClient, err := NewHTTPClient(proxy)
	if err != nil {
		return nil, err
	}
	return &Client{
		token:      token,
		httpClient: httpClient,
		logger:     logger,
		APIBase:    DefaultAPIBase,
		UploadBase: DefaultUploadBase,
		DeviceURL:  deviceCodeURL,
		TokenURL:   accessTokenURL,
		UserAgent:  "DSHBackup/1.0",
	}, nil
}

func NewHTTPClient(proxyCfg ProxyConfig) (*http.Client, error) {
	proxyURL, err := resolveProxy(proxyCfg)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy: func(request *http.Request) (*url.URL, error) {
			if proxyURL == nil {
				return nil, nil
			}
			if proxyCfg.BypassLocal && isLocalHost(request.URL.Hostname()) {
				return nil, nil
			}
			return proxyURL, nil
		},
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   20 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{Transport: transport}, nil
}

func resolveProxy(cfg ProxyConfig) (*url.URL, error) {
	raw := ""
	switch cfg.Mode {
	case "off":
		return nil, nil
	case "system":
		system, err := platform.SystemProxy()
		if err != nil {
			return nil, fmt.Errorf("读取系统代理失败：%w", err)
		}
		raw = system
	case "manual":
		raw = cfg.URL
	default:
		return nil, fmt.Errorf("未知代理模式：%s", cfg.Mode)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("代理地址无效：%s", cfg.URL)
	}
	if cfg.Username != "" {
		parsed.User = url.UserPassword(cfg.Username, cfg.Password)
	}
	return parsed, nil
}

func isLocalHost(host string) bool {
	lower := strings.ToLower(host)
	if lower == "localhost" || lower == "127.0.0.1" || lower == "::1" {
		return true
	}
	if strings.HasPrefix(lower, "127.") || strings.HasSuffix(lower, ".localhost") {
		return true
	}
	return false
}

func ProxyConfigFrom(cfg config.Config, password string) ProxyConfig {
	return ProxyConfig{
		Mode:        cfg.Proxy.Mode,
		URL:         cfg.Proxy.URL,
		Username:    cfg.Proxy.Username,
		Password:    password,
		BypassLocal: cfg.Proxy.BypassLocal,
	}
}

type apiResponse struct {
	status int
	body   []byte
	header http.Header
}

func (c *Client) apiURL(path string) string {
	return strings.TrimRight(c.APIBase, "/") + path
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, result any) error {
	var payload []byte
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = raw
	}
	response, err := c.doWithRetry(ctx, method, c.apiURL(path), payload, true)
	if err != nil {
		return err
	}
	if result != nil && len(response.body) > 0 {
		if err := json.Unmarshal(response.body, result); err != nil {
			return fmt.Errorf("解析 GitHub 响应失败：%w", err)
		}
	}
	return nil
}

func (c *Client) doWithRetry(ctx context.Context, method, rawURL string, payload []byte, auth bool) (apiResponse, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			delay := time.Duration(1<<uint(attempt-1)) * time.Second
			if apiErr, ok := lastErr.(*APIError); ok && apiErr.RetryAfter > 0 {
				delay = apiErr.RetryAfter
			}
			if err := sleepContext(ctx, delay); err != nil {
				return apiResponse{}, err
			}
		}
		request, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(payload))
		if err != nil {
			return apiResponse{}, err
		}
		c.setHeaders(request, auth, len(payload) > 0)
		response, err := c.httpClient.Do(request)
		if err != nil {
			lastErr = fmt.Errorf("连接 GitHub 失败：%w", err)
			if ctx.Err() != nil {
				return apiResponse{}, ctx.Err()
			}
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
		_ = response.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if c.logger != nil {
			c.logger.Info("github.request", "", map[string]any{
				"method": method,
				"url":    redactURL(rawURL),
				"status": response.StatusCode,
			})
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return apiResponse{status: response.StatusCode, body: body, header: response.Header}, nil
		}
		apiErr := c.translateError(response.StatusCode, response.Header, body)
		lastErr = apiErr
		if !retryableStatus(response.StatusCode) {
			if response.StatusCode == 403 && (response.Header.Get("X-RateLimit-Remaining") == "0" || response.Header.Get("Retry-After") != "") {
				continue
			}
			return apiResponse{}, apiErr
		}
	}
	return apiResponse{}, lastErr
}

func (c *Client) setHeaders(request *http.Request, auth, jsonBody bool) {
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", c.UserAgent)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if jsonBody {
		request.Header.Set("Content-Type", "application/json")
	}
	if auth && c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
}

func retryableStatus(status int) bool {
	return status == 429 || status == 502 || status == 503 || status == 504 || status == 500
}

func (c *Client) translateError(status int, header http.Header, body []byte) *APIError {
	message := ""
	var payload struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		message = payload.Message
		for _, item := range payload.Errors {
			if item.Message != "" {
				message += "：" + item.Message
			}
		}
	}
	var friendly string
	switch status {
	case 401:
		friendly = "GitHub token 无效或已过期，请重新连接"
	case 403:
		if header.Get("X-RateLimit-Remaining") == "0" {
			friendly = "GitHub API 限流，请稍后重试"
		} else {
			friendly = "GitHub 拒绝了请求：权限不足，或令牌缺少 repo scope"
		}
	case 404:
		friendly = "找不到仓库或资源；请确认私有仓库存在且 token 有访问权限"
	case 409:
		friendly = "仓库状态冲突，请先初始化 README"
	case 422:
		friendly = "GitHub 拒绝了这次操作；请检查仓库是否为空、权限是否足够"
	case 429:
		friendly = "GitHub 限流，请稍后重试"
	default:
		if status >= 500 {
			friendly = "GitHub 服务暂时不可用，请稍后重试"
		} else {
			friendly = fmt.Sprintf("GitHub 返回 HTTP %d", status)
		}
	}
	if message != "" && message != "Validation Failed" {
		friendly += "：" + message
	}
	retryAfter := time.Duration(0)
	if raw := header.Get("Retry-After"); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil {
			retryAfter = time.Duration(seconds) * time.Second
		}
	}
	if reset := header.Get("X-RateLimit-Reset"); reset != "" {
		if epoch, err := strconv.ParseInt(reset, 10, 64); err == nil {
			if delay := time.Until(time.Unix(epoch, 0)); delay > 0 && delay < time.Hour {
				retryAfter = delay
			}
		}
	}
	return &APIError{
		Status:     status,
		Message:    friendly,
		RequestID:  header.Get("X-GitHub-Request-Id"),
		RetryAfter: retryAfter,
	}
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func redactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return parsed.Scheme + "://" + parsed.Host + parsed.Path
}

type progressReader struct {
	reader io.Reader
	total  int64
	done   int64
	last   time.Time
	mu     sync.Mutex
	report func(done, total int64)
}

func (p *progressReader) Read(buffer []byte) (int, error) {
	read, err := p.reader.Read(buffer)
	p.mu.Lock()
	p.done += int64(read)
	shouldReport := time.Since(p.last) > 200*time.Millisecond
	if shouldReport {
		p.last = time.Now()
	}
	done := p.done
	p.mu.Unlock()
	if shouldReport && p.report != nil {
		p.report(done, p.total)
	}
	return read, err
}

func (p *progressReader) finish() {
	if p.report != nil {
		p.report(p.total, p.total)
	}
}
