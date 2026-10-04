//go:build windows

package ui

import (
	"fmt"
	"os/exec"
	"syscall"
)

// playBuiltinSound 在 Windows 上播放内置提示音（见需求 3）。
//
// 做法：合成 WAV 写到临时文件，用 PowerShell 的 SoundPlayer 播放。
// 选它是因为 Windows 自带、不需要引第三方音频库，也不会把二进制撑大。
//
// 三个刻意的取舍：
//   - 用 `cmd /c start /b` 把播放**完全脱离**当前进程：TUI 绝不能被音频阻塞。
//   - 播放**尽力而为**：任何失败都只是没声音，返回错误由界面提示一次，不影响计时。
//   - 用 Start（异步）而不是 PlaySync：即使脱离失败也不会卡住。
func playBuiltinSound(name string) error {
	path, err := builtinSoundFile(name)
	if err != nil {
		return err
	}
	// 单引号里的路径：Windows 路径不会含单引号，这里无需再转义。
	script := fmt.Sprintf("(New-Object Media.SoundPlayer '%s').Play()", path)

	cmd := exec.Command("cmd", "/c", "start", "/b", "",
		"powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden",
		"-Command", script)
	// 创建新进程组并隐藏窗口：不要在用户终端上闪出一个黑框。
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000008 | 0x00000200, // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动提示音播放失败: %w", err)
	}
	// 不 Wait：进程已脱离，界面不等它。由系统在播放结束后回收。
	return nil
}
