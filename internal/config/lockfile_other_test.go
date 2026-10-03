//go:build !windows

package config

import "errors"

// fileLock 在非 Windows 平台上的占位实现。
type fileLock struct{}

// lockFileExclusive 在非 Windows 上不可用。
//
// POSIX 的 rename 可以覆盖被打开的文件，不存在 Windows 那种共享冲突，
// 所以相关测试会跳过。
func lockFileExclusive(string) (*fileLock, error) {
	return nil, errors.New("非 Windows 平台不支持独占锁模拟")
}

func (l *fileLock) close() {}
