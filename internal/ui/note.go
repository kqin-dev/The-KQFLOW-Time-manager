package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// openNote 打开当天的随手记编辑器。
//
// 复用中间栏当多行编辑器：Enter 换行，Ctrl+S 保存，Esc 取消。
// 按键刻意避开 Enter，否则“写日记”和“确认”会互相打架。
func (a *App) openNote() {
	a.editor.setMultiline(fmt.Sprintf("随手记 · %s（ctrl+s 保存）", a.day), a.data.Note)
	a.editor.onCommit = func(value string) (tea.Model, tea.Cmd) {
		// 只保留右侧空白的清理，行首缩进保留（用户可能刻意排版）。
		cleaned := strings.TrimRight(strings.ReplaceAll(value, "\r\n", "\n"), "\n")
		a.data.Note = cleaned
		if err := a.saveDay(); err != nil {
			a.setToast("随手记保存失败："+err.Error(), toastErr)
			return a, nil
		}
		if strings.TrimSpace(cleaned) == "" {
			a.setToast("随手记已清空", toastInfo)
		} else {
			lines := strings.Count(cleaned, "\n") + 1
			a.setToast(fmt.Sprintf("已保存随手记（%d 行）", lines), toastInfo)
		}
		return a, nil
	}
}

// toggleShowNote 切换是否在看板上展示随手记。
func (a *App) toggleShowNote() {
	a.cfg.ShowNote = !a.cfg.ShowNote
	a.saveConfig()
	if a.cfg.ShowNote {
		a.setToast("看板将展示随手记的前几行", toastInfo)
	} else {
		a.setToast("看板不再展示随手记", toastInfo)
	}
}
