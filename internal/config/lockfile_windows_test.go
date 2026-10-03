//go:build windows

package config

import (
	"os"

	"golang.org/x/sys/windows"
)

// fileLock 是一个独占文件锁，用于在测试里复现“目标文件被别的进程占用”。
type fileLock struct{ f *os.File }

// lockFileExclusive 以独占方式（不共享读写）打开文件。
//
// 这样在锁释放前，MoveFileEx(MOVEFILE_REPLACE_EXISTING) 会以
// Access is denied 失败——正是用户遇到的那个错误。
//
// 刻意只用 GENERIC_READ：带 GENERIC_WRITE 打开会把文件截断成 0 字节，
// 那样就分不清“rename 失败”和“文件本来就被清空了”。
func lockFileExclusive(path string) (*fileLock, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(
		p,
		windows.GENERIC_READ,
		0, // 不共享：独占
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	return &fileLock{f: os.NewFile(uintptr(h), path)}, nil
}

func (l *fileLock) close() {
	if l != nil && l.f != nil {
		_ = l.f.Close()
	}
}
