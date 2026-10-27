package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type DeviceFlowInfo struct {
	DeviceCode              string `json:"deviceCode"`
	UserCode                string `json:"userCode"`
	VerificationURI         string `json:"verificationUri"`
	VerificationURIComplete string `json:"verificationUriComplete,omitempty"`
	ExpiresIn               int    `json:"expiresIn"`
	Interval                int    `json:"interval"`
}

type Account struct {
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
	HTMLURL   string `json:"htmlUrl"`
}

func (c *Client) StartDeviceFlow(ctx context.Context, clientID string) (DeviceFlowInfo, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return DeviceFlowInfo{}, fmt.Errorf("未配置 OAuth App Client ID")
	}
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("scope", "repo")
	var payload struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
		Error                   string `json:"error"`
		ErrorDescription        string `json:"error_description"`
	}
	if err := c.postForm(ctx, c.DeviceURL, form, &payload, false); err != nil {
		return DeviceFlowInfo{}, err
	}
	if payload.Error != "" {
		return DeviceFlowInfo{}, fmt.Errorf("GitHub 拒绝设备授权：%s", payload.ErrorDescription)
	}
	if payload.DeviceCode == "" || payload.UserCode == "" {
		return DeviceFlowInfo{}, fmt.Errorf("GitHub 未返回设备授权码")
	}
	if payload.ExpiresIn <= 0 {
		payload.ExpiresIn = 900
	}
	if payload.Interval <= 0 {
		payload.Interval = 5
	}
	return DeviceFlowInfo{
		DeviceCode:              payload.DeviceCode,
		UserCode:                payload.UserCode,
		VerificationURI:         payload.VerificationURI,
		VerificationURIComplete: payload.VerificationURIComplete,
		ExpiresIn:               payload.ExpiresIn,
		Interval:                payload.Interval,
	}, nil
}

func (c *Client) PollDeviceFlow(
	ctx context.Context,
	clientID string,
	info DeviceFlowInfo,
	onStatus func(status, message string),
) (string, error) {
	interval := time.Duration(info.Interval) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(info.ExpiresIn) * time.Second)
	if onStatus != nil {
		onStatus("waiting", "等待你在浏览器中完成授权")
	}
	for {
		if time.Now().After(deadline) {
			return "", fmt.Errorf("GitHub 授权码已过期，请重新连接")
		}
		if err := sleepContext(ctx, interval); err != nil {
			return "", err
		}
		form := url.Values{}
		form.Set("client_id", clientID)
		form.Set("device_code", info.DeviceCode)
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		var payload struct {
			AccessToken      string `json:"access_token"`
			TokenType        string `json:"token_type"`
			Scope            string `json:"scope"`
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
			Interval         int    `json:"interval"`
		}
		if err := c.postForm(ctx, c.TokenURL, form, &payload, false); err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if onStatus != nil {
				onStatus("waiting", "网络暂时不可用，正在重试授权检查")
			}
			continue
		}
		if payload.AccessToken != "" {
			if onStatus != nil {
				onStatus("success", "GitHub 授权成功")
			}
			return payload.AccessToken, nil
		}
		switch payload.Error {
		case "authorization_pending":
			if onStatus != nil {
				onStatus("waiting", "等待你在浏览器中完成授权")
			}
		case "slow_down":
			interval += 5 * time.Second
			if onStatus != nil {
				onStatus("waiting", "GitHub 要求降低轮询频率")
			}
		case "expired_token":
			return "", fmt.Errorf("GitHub 授权码已过期，请重新连接")
		case "access_denied":
			return "", fmt.Errorf("你取消了 GitHub 授权")
		case "incorrect_device_code":
			return "", fmt.Errorf("GitHub 设备码无效，请重新连接")
		case "":
			return "", fmt.Errorf("GitHub 返回了未知的授权结果")
		default:
			description := payload.ErrorDescription
			if description == "" {
				description = payload.Error
			}
			return "", fmt.Errorf("GitHub 授权失败：%s", description)
		}
	}
}

func (c *Client) Account(ctx context.Context) (Account, error) {
	var payload struct {
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
		HTMLURL   string `json:"html_url"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/user", nil, &payload); err != nil {
		return Account{}, err
	}
	return Account{
		Login:     payload.Login,
		Name:      payload.Name,
		AvatarURL: payload.AvatarURL,
		HTMLURL:   payload.HTMLURL,
	}, nil
}

func (c *Client) postForm(ctx context.Context, rawURL string, form url.Values, result any, auth bool) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if err := sleepContext(ctx, time.Duration(attempt)*time.Second); err != nil {
				return err
			}
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form.Encode()))
		if err != nil {
			return err
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("User-Agent", c.UserAgent)
		if auth && c.token != "" {
			request.Header.Set("Authorization", "Bearer "+c.token)
		}
		response, err := c.httpClient.Do(request)
		if err != nil {
			lastErr = fmt.Errorf("连接 GitHub 失败：%w", err)
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024))
		_ = response.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			lastErr = c.translateError(response.StatusCode, response.Header, body)
			if !retryableStatus(response.StatusCode) {
				return lastErr
			}
			continue
		}
		if err := json.Unmarshal(body, result); err != nil {
			return fmt.Errorf("解析 GitHub 授权响应失败：%w", err)
		}
		return nil
	}
	return lastErr
}

type Connectivity struct {
	Reachable bool   `json:"reachable"`
	Status    int    `json:"status"`
	LatencyMS int64  `json:"latencyMs"`
	Message   string `json:"message"`
}

func (c *Client) Ping(ctx context.Context) (Connectivity, error) {
	start := time.Now()
	response, err := c.doWithRetry(ctx, http.MethodGet, c.apiURL("/"), nil, false)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return Connectivity{Reachable: false, LatencyMS: latency, Message: err.Error()}, err
	}
	return Connectivity{
		Reachable: true,
		Status:    response.status,
		LatencyMS: latency,
		Message:   "GitHub API 可访问",
	}, nil
}
