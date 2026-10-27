//go:build windows

package creds

import (
	"errors"
	"fmt"

	"github.com/danieljoos/wincred"
)

const (
	GitHubTokenTarget   = "DSHBackup/github-token"
	ProxyPasswordTarget = "DSHBackup/proxy-password"
)

func Set(target, username, secret string) error {
	if secret == "" {
		return Delete(target)
	}
	credential := wincred.NewGenericCredential(target)
	credential.UserName = username
	credential.CredentialBlob = []byte(secret)
	credential.Persist = wincred.PersistLocalMachine
	if err := credential.Write(); err != nil {
		return fmt.Errorf("写入 Windows 凭据管理器失败：%w", err)
	}
	return nil
}

func Get(target string) (string, error) {
	credential, err := wincred.GetGenericCredential(target)
	if err != nil {
		if errors.Is(err, wincred.ErrElementNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("读取 Windows 凭据管理器失败：%w", err)
	}
	return string(credential.CredentialBlob), nil
}

func Exists(target string) bool {
	secret, err := Get(target)
	return err == nil && secret != ""
}

func Delete(target string) error {
	credential, err := wincred.GetGenericCredential(target)
	if err != nil {
		if errors.Is(err, wincred.ErrElementNotFound) {
			return nil
		}
		return err
	}
	if err := credential.Delete(); err != nil && !errors.Is(err, wincred.ErrElementNotFound) {
		return err
	}
	return nil
}
