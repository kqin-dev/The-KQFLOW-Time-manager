//go:build !windows

package ui

import "fmt"

// playBuiltinSound 在非 Windows 平台上是空操作。
//
// 明确返回错误而不是静默成功：界面会提示一次「当前系统不支持内置提示音」，
// 用户可以选择换成「终端响铃」预设，而不是以为程序坏了。
func playBuiltinSound(name string) error {
	return fmt.Errorf("当前系统不支持内置提示音，可改用「终端响铃」")
}
