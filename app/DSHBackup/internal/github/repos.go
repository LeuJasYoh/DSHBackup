package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type Repo struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"fullName"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"defaultBranch"`
	HTMLURL       string `json:"htmlUrl"`
	Owner         string `json:"owner"`
	CanPush       bool   `json:"canPush"`
	UpdatedAt     string `json:"updatedAt"`
}

type repoPayload struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
	UpdatedAt     string `json:"updated_at"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
	Permissions struct {
		Push bool `json:"push"`
	} `json:"permissions"`
}

func convertRepo(payload repoPayload) Repo {
	return Repo{
		ID:            payload.ID,
		Name:          payload.Name,
		FullName:      payload.FullName,
		Private:       payload.Private,
		DefaultBranch: payload.DefaultBranch,
		HTMLURL:       payload.HTMLURL,
		Owner:         payload.Owner.Login,
		CanPush:       payload.Permissions.Push,
		UpdatedAt:     payload.UpdatedAt,
	}
}

func (c *Client) ListRepos(ctx context.Context) ([]Repo, error) {
	var repos []Repo
	for page := 1; page <= 5; page++ {
		var payload []repoPayload
		path := fmt.Sprintf("/user/repos?per_page=100&sort=updated&affiliation=owner&page=%d", page)
		if err := c.doJSON(ctx, http.MethodGet, path, nil, &payload); err != nil {
			return nil, err
		}
		for _, item := range payload {
			repos = append(repos, convertRepo(item))
		}
		if len(payload) < 100 {
			break
		}
	}
	return repos, nil
}

func (c *Client) GetRepo(ctx context.Context, owner, repo string) (Repo, error) {
	var payload repoPayload
	path := fmt.Sprintf("/repos/%s/%s", url.PathEscape(owner), url.PathEscape(repo))
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return Repo{}, err
	}
	return convertRepo(payload), nil
}

func (c *Client) CreateRepo(ctx context.Context, name string) (Repo, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Repo{}, fmt.Errorf("仓库名称不能为空")
	}
	body := map[string]any{
		"name":        name,
		"private":     true,
		"auto_init":   true,
		"description": "DSH Backup encrypted snapshots",
	}
	var payload repoPayload
	if err := c.doJSON(ctx, http.MethodPost, "/user/repos", body, &payload); err != nil {
		return Repo{}, err
	}
	return convertRepo(payload), nil
}

func (c *Client) EnsureRepoReady(ctx context.Context, owner, repo string) (Repo, error) {
	value, err := c.GetRepo(ctx, owner, repo)
	if err != nil {
		return Repo{}, err
	}
	if !value.Private {
		return Repo{}, fmt.Errorf("仓库 %s 是公开仓库；备份必须使用私有仓库", value.FullName)
	}
	if !value.CanPush {
		return Repo{}, fmt.Errorf("当前 GitHub 账号对仓库 %s 没有写入权限", value.FullName)
	}
	if value.DefaultBranch == "" {
		return Repo{}, fmt.Errorf("仓库 %s 还没有初始化分支；请先在 GitHub 创建 README", value.FullName)
	}
	return value, nil
}
