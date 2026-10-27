package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"DSHBackup/internal/config"
	"DSHBackup/internal/creds"
	"DSHBackup/internal/github"
)

func (a *App) newGitHubClient(requireToken bool) (*github.Client, string, error) {
	token := ""
	if requireToken {
		value, err := creds.Get(creds.GitHubTokenTarget)
		if err != nil {
			return nil, "", err
		}
		if value == "" {
			return nil, "", errors.New("尚未连接 GitHub")
		}
		token = value
	}
	password, _ := creds.Get(creds.ProxyPasswordTarget)
	cfg := a.configSnapshot()
	client, err := github.New(token, github.ProxyConfigFrom(cfg, password), a.logger)
	if err != nil {
		return nil, "", err
	}
	return client, token, nil
}

func (a *App) StartGitHubDeviceFlow(clientID string) (DeviceFlowView, error) {
	cfg := a.configSnapshot()
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = cfg.GitHub.ClientID
	}
	if clientID == "" {
		return DeviceFlowView{}, errors.New("请先填写 GitHub OAuth App 的 Client ID；也可以在高级选项中使用 PAT")
	}
	client, _, err := a.newGitHubClient(false)
	if err != nil {
		return DeviceFlowView{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	info, err := client.StartDeviceFlow(ctx, clientID)
	cancel()
	if err != nil {
		return DeviceFlowView{}, err
	}
	if cfg.GitHub.ClientID != clientID {
		cfg.GitHub.ClientID = clientID
		if saveErr := config.Save(a.paths.ConfigFile(), cfg); saveErr == nil {
			a.setConfig(cfg)
		}
	}
	a.cancelGitHubAuth()
	authCtx, authCancel := context.WithCancel(context.Background())
	a.authMu.Lock()
	a.authCancel = authCancel
	a.authMu.Unlock()
	expiresAt := time.Now().Add(time.Duration(info.ExpiresIn) * time.Second)
	view := DeviceFlowView{
		UserCode:                info.UserCode,
		VerificationURI:         info.VerificationURI,
		VerificationURIComplete: info.VerificationURIComplete,
		ExpiresIn:               info.ExpiresIn,
		Interval:                info.Interval,
		ExpiresAt:               expiresAt.Format(time.RFC3339),
	}
	a.emitGitHubAuth(GitHubAuthEvent{
		Status:          "waiting",
		UserCode:        info.UserCode,
		VerificationURI: info.VerificationURI,
		ExpiresAt:       view.ExpiresAt,
		Message:         "等待你在浏览器中完成授权",
	})
	if a.ctx != nil {
		target := info.VerificationURIComplete
		if target == "" {
			target = info.VerificationURI
		}
		runtime.BrowserOpenURL(a.ctx, target)
	}
	go func() {
		token, pollErr := client.PollDeviceFlow(authCtx, clientID, info, func(status, message string) {
			a.emitGitHubAuth(GitHubAuthEvent{
				Status:          status,
				UserCode:        info.UserCode,
				VerificationURI: info.VerificationURI,
				ExpiresAt:       view.ExpiresAt,
				Message:         message,
			})
		})
		if pollErr != nil {
			if authCtx.Err() != nil {
				a.emitGitHubAuth(GitHubAuthEvent{Status: "cancelled", Message: "已取消 GitHub 授权"})
			} else {
				a.emitGitHubAuth(GitHubAuthEvent{Status: "error", Message: pollErr.Error()})
			}
			a.clearAuthCancel()
			return
		}
		accountClientWithToken, err := github.New(token, accountClientProxy(a), a.logger)
		if err != nil {
			a.emitGitHubAuth(GitHubAuthEvent{Status: "error", Message: err.Error()})
			a.clearAuthCancel()
			return
		}
		accountCtx, cancelAccount := context.WithTimeout(context.Background(), 25*time.Second)
		account, accountErr := accountClientWithToken.Account(accountCtx)
		cancelAccount()
		if accountErr != nil {
			a.emitGitHubAuth(GitHubAuthEvent{Status: "error", Message: "授权成功但读取 GitHub 账号失败：" + accountErr.Error()})
			a.clearAuthCancel()
			return
		}
		if err := creds.Set(creds.GitHubTokenTarget, "GitHub Device Flow", token); err != nil {
			a.emitGitHubAuth(GitHubAuthEvent{Status: "error", Message: err.Error()})
			a.clearAuthCancel()
			return
		}
		if err := a.applyGitHubAccount(account); err != nil {
			_ = creds.Delete(creds.GitHubTokenTarget)
			a.emitGitHubAuth(GitHubAuthEvent{Status: "error", Message: err.Error()})
			a.clearAuthCancel()
			return
		}
		a.emitGitHubAuth(GitHubAuthEvent{
			Status:  "success",
			Message: "GitHub 已连接",
			Account: &account,
		})
		a.emitStateChanged("github")
		a.invalidateCloudCache()
		a.clearAuthCancel()
	}()
	return view, nil
}

func accountClientProxy(a *App) github.ProxyConfig {
	password, _ := creds.Get(creds.ProxyPasswordTarget)
	return github.ProxyConfigFrom(a.configSnapshot(), password)
}

func (a *App) applyGitHubAccount(account github.Account) error {
	cfg := a.configSnapshot()
	cfg.GitHub.Login = account.Login
	cfg.GitHub.AvatarURL = account.AvatarURL
	cfg.GitHub.ConnectedAt = time.Now().Format(time.RFC3339)
	if err := config.Save(a.paths.ConfigFile(), cfg); err != nil {
		return fmt.Errorf("保存 GitHub 账号信息失败：%w", err)
	}
	a.setConfig(cfg)
	return nil
}

func (a *App) ConnectGitHubPAT(token string) (GitHubStatus, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return GitHubStatus{}, errors.New("PAT 不能为空")
	}
	cfg := a.configSnapshot()
	password, _ := creds.Get(creds.ProxyPasswordTarget)
	client, err := github.New(token, github.ProxyConfigFrom(cfg, password), a.logger)
	if err != nil {
		return GitHubStatus{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	account, err := client.Account(ctx)
	cancel()
	if err != nil {
		return GitHubStatus{}, err
	}
	if err := creds.Set(creds.GitHubTokenTarget, "GitHub PAT", token); err != nil {
		return GitHubStatus{}, err
	}
	cfg.GitHub.Login = account.Login
	cfg.GitHub.AvatarURL = account.AvatarURL
	cfg.GitHub.ConnectedAt = time.Now().Format(time.RFC3339)
	if err := config.Save(a.paths.ConfigFile(), cfg); err != nil {
		_ = creds.Delete(creds.GitHubTokenTarget)
		return GitHubStatus{}, err
	}
	a.setConfig(cfg)
	a.invalidateCloudCache()
	a.emitStateChanged("github")
	return a.gitHubStatus(cfg), nil
}

func (a *App) DisconnectGitHub() error {
	a.cancelGitHubAuth()
	if err := creds.Delete(creds.GitHubTokenTarget); err != nil {
		return err
	}
	cfg := a.configSnapshot()
	cfg.GitHub.Login = ""
	cfg.GitHub.AvatarURL = ""
	cfg.GitHub.ConnectedAt = ""
	if cfg.Mode != config.ModeLocal {
		cfg.Mode = config.ModeLocal
	}
	if err := config.Save(a.paths.ConfigFile(), cfg); err != nil {
		return err
	}
	a.setConfig(cfg)
	a.invalidateCloudCache()
	a.emitGitHubAuth(GitHubAuthEvent{Status: "disconnected", Message: "已断开 GitHub 连接"})
	a.emitStateChanged("github")
	return nil
}

func (a *App) ListGitHubRepos(query string) ([]github.Repo, error) {
	client, _, err := a.newGitHubClient(true)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	repos, err := client.ListRepos(ctx)
	cancel()
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	var filtered []github.Repo
	for _, repo := range repos {
		if !repo.Private {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(repo.FullName), query) {
			continue
		}
		filtered = append(filtered, repo)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].UpdatedAt > filtered[j].UpdatedAt
	})
	return filtered, nil
}

func (a *App) CreateGitHubRepo(name string) (github.Repo, error) {
	client, _, err := a.newGitHubClient(true)
	if err != nil {
		return github.Repo{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	repo, err := client.CreateRepo(ctx, name)
	cancel()
	if err != nil {
		return github.Repo{}, err
	}
	return repo, nil
}

func (a *App) SelectGitHubRepo(fullName string) (github.Repo, error) {
	fullName = strings.TrimSpace(fullName)
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return github.Repo{}, errors.New("仓库格式必须为 owner/repo")
	}
	owner := strings.TrimSpace(parts[0])
	repoName := strings.TrimSpace(parts[1])
	client, _, err := a.newGitHubClient(true)
	if err != nil {
		return github.Repo{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	repo, err := client.EnsureRepoReady(ctx, owner, repoName)
	cancel()
	if err != nil {
		return github.Repo{}, err
	}
	cfg := a.configSnapshot()
	cfg.GitHub.Owner = owner
	cfg.GitHub.Repo = repoName
	cfg.GitHub.ConnectedAt = time.Now().Format(time.RFC3339)
	if err := config.Save(a.paths.ConfigFile(), cfg); err != nil {
		return github.Repo{}, err
	}
	a.setConfig(cfg)
	a.invalidateCloudCache()
	a.emitStateChanged("github")
	return repo, nil
}

func (a *App) cancelGitHubAuth() {
	a.authMu.Lock()
	cancel := a.authCancel
	a.authCancel = nil
	a.authMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) CancelGitHubAuth() {
	a.cancelGitHubAuth()
	a.emitGitHubAuth(GitHubAuthEvent{Status: "cancelled", Message: "已取消 GitHub 授权"})
}

func (a *App) clearAuthCancel() {
	a.authMu.Lock()
	a.authCancel = nil
	a.authMu.Unlock()
}

func (a *App) emitGitHubAuth(event GitHubAuthEvent) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "github:auth", event)
	}
}
