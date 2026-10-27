//go:build !windows

package creds

import "errors"

const (
	GitHubTokenTarget   = "DSHBackup/github-token"
	ProxyPasswordTarget = "DSHBackup/proxy-password"
)

func Set(_, _, _ string) error     { return errors.New("凭据管理器仅支持 Windows") }
func Get(_ string) (string, error) { return "", errors.New("凭据管理器仅支持 Windows") }
func Exists(_ string) bool         { return false }
func Delete(_ string) error        { return nil }
