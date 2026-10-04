package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/config"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// 时段切换提醒（见需求 3）。
//
// 需求把提醒按「注意力距离」分成三档，这里是这三档的调度中心：
//   - 人在屏幕前、静音 → 流光特效（中间栏一行）
//   - 人在设备附近但没看屏幕 → 提示音
//   - 人离开设备、只带了手机 → ntfy 推送
//
// 三者各自可关（配置为空 / none 即关闭），默认全关。

// notifyGlowPresets 是可选流光特效（需求要求提供几种预设，含关闭）。
var notifyGlowPresets = []struct {
	Key   string
	Label string
}{
	{"none", "关闭"},
	{"aurora", "极光（横向扫过的渐变光带）"},
	{"pulse", "脉动（整行明暗呼吸）"},
	{"wipe", "扫描（从左到右的亮块）"},
}

// notifySoundPresets 是可选提示音（需求要求提供几个预制的，含无声音）。
//
// "bell" 只用终端响铃：不需要任何音频能力，远程终端 / 精简系统上也能用。
// 其余是程序合成的短音频（见 notify_sound.go）。
var notifySoundPresets = []struct {
	Key   string
	Label string
}{
	{"none", "关闭"},
	{"bell", "终端响铃（无需音频支持）"},
	{"bowl", "颂钵（低频、长衰减）"},
	{"chime", "风铃（清脆、带泛音）"},
	{"white", "白噪音（一秒沙沙声，淡出）"},
}

// notifyDuration 是流光特效持续时长。
//
// 需求要求「足够醒目又没有很强的割裂感」：太短看不见，太长会一直占着中间栏。
const notifyDuration = 2500 * time.Millisecond

// notifyState 保存一次时段切换提醒的播放状态。
type notifyState struct {
	started time.Time
	// kind 是切换到的时段类型，用来决定配色与推送措辞。
	kind string
	// toName 是切换到的时段名，用于提示文案与推送标题。
	toName string
	// pushCmd 是后台推送命令（未启用 ntfy 时为 nil）。
	pushCmd tea.Cmd
}

// ---------- 触发 ----------

// checkSegmentChange 在推进计时时检查是否跨过了时段边界。
//
// timerState.lastSeg 记录上一次推进所在的段；这次不同即视为「切换了时段」。
// 只在**向前**跨段时提醒：暂停、回退、以及首次推进（lastSeg 为 -1）都不提醒，
// 否则按一下空格暂停再继续就会又响一次。
func (a *App) checkSegmentChange(now time.Time) *notifyState {
	if a.timer == nil {
		return nil
	}
	idx, seg, _ := a.timer.segment(now)
	prev := a.timer.lastSeg
	a.timer.lastSeg = idx
	if prev < 0 || idx == prev {
		return nil
	}
	if !a.notifyEnabled() {
		return nil
	}
	return &notifyState{
		started: now,
		kind:    seg.Kind,
		toName:  seg.Name,
		pushCmd: a.ntfyPushCmd(seg),
	}
}

// notifyEnabled 报告是否至少开了一种提醒。
func (a *App) notifyEnabled() bool {
	if a.cfg == nil {
		return false
	}
	return a.cfg.EffectiveNotifyGlow() != "" ||
		a.cfg.EffectiveNotifySound() != "" ||
		a.cfg.NtfyReady()
}

// ---------- 流光特效 ----------

// notifyProgress 返回流光特效的播放进度（0..1）；没在播放时返回 1。
func (a *App) notifyProgress(now time.Time) float64 {
	if a.notifyState == nil {
		return 1
	}
	p := float64(now.Sub(a.notifyState.started)) / float64(notifyDuration)
	if p < 0 {
		return 0
	}
	if p > 1 {
		return 1
	}
	return p
}

// notifyDone 报告流光是否播完。
func (a *App) notifyDone(now time.Time) bool {
	return a.notifyState == nil || now.Sub(a.notifyState.started) >= notifyDuration
}

// notifyFrame 渲染一次流光；width 是中间栏内容宽度。
//
// 刻意做成「一行浮动光带」而不是整屏反色：整屏反色在真实终端里观感很割裂
// （需求明确说不要），而且会盖掉进度条与计时。这里只占一行，左右面板不动。
func (a *App) notifyFrame(progress float64, width int) string {
	if width < 8 || a.notifyState == nil {
		return ""
	}
	preset := a.cfg.EffectiveNotifyGlow()
	if preset == "" {
		return ""
	}
	progress = clamp01(progress)

	// 淡入淡出：两端各占 25%，避免突然出现又突然消失。
	fade := 1.0
	switch {
	case progress < 0.25:
		fade = progress / 0.25
	case progress > 0.75:
		fade = (1 - progress) / 0.25
	}
	fade = quantizeFade(fade)

	base, accent := a.st.Theme.Primary, a.st.Theme.Success
	switch a.notifyState.kind {
	case model.SegmentKindBreak:
		base, accent = a.st.Theme.Warning, a.st.Theme.Secondary
	case "other":
		base, accent = a.st.Theme.Secondary, a.st.Theme.Primary
	}
	dim := a.fadeStyle(base, fade*0.35)
	mid := a.fadeStyle(base, fade)
	hot := a.fadeStyle(accent, fade)

	switch preset {
	case "pulse":
		head := " ◈ " + a.notifyState.toName + " 即将开始 "
		if lipgloss.Width(head) >= width {
			return a.st.Accent.Bold(true).Render(truncate(head, width))
		}
		body := strings.Repeat("─", width-lipgloss.Width(head))
		return mid.Render(head + body)

	case "wipe":
		pos := int(progress * float64(width))
		var b strings.Builder
		for i := 0; i < width; i++ {
			if abs(i-pos) <= 3 {
				b.WriteString(hot.Render("━"))
			} else {
				b.WriteString(dim.Render("─"))
			}
		}
		return b.String()

	default: // aurora
		head := " ◈ " + a.notifyState.toName + " 即将开始 "
		headW := lipgloss.Width(head)
		if headW >= width {
			return a.st.Accent.Bold(true).Render(truncate(head, width))
		}
		body := width - headW
		pos := int(progress * float64(body))
		var b strings.Builder
		for i := 0; i < body; i++ {
			d := abs(i - pos)
			switch {
			case d <= 2:
				b.WriteString(hot.Render("━"))
			case d <= 6:
				b.WriteString(mid.Render("━"))
			default:
				b.WriteString(dim.Render("─"))
			}
		}
		return a.st.Accent.Bold(true).Render(head) + b.String()
	}
}

// fadeStyle 返回一个按系数往背景色靠拢的颜色样式。
//
// 终端颜色没有 alpha，这里用「朝背景插值」模拟渐隐：系数越小越接近背景，
// 看起来就是淡出。用 hex 分量线性插值，避免引入额外依赖。
func (a *App) fadeStyle(c lipgloss.Color, factor float64) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(blendHex(
		string(c), string(a.st.Theme.Bg), clamp01(factor),
	)))
}

// quantizeFade 把连续淡入淡出量化到有限档位，减少每帧生成的颜色数量。
func quantizeFade(f float64) float64 {
	const steps = 6
	if f <= 0 {
		return 0
	}
	if f >= 1 {
		return 1
	}
	return float64(int(f*steps+0.999)) / steps
}

// blendHex 把前景色按 t（0..1）混向背景色；解析失败时原样返回前景色。
func blendHex(fg, bg string, t float64) string {
	fr, fg2, fb, ok1 := parseHex(fg)
	br, bg2, bb, ok2 := parseHex(bg)
	if !ok1 || !ok2 {
		return fg
	}
	mix := func(f, b int) int {
		v := float64(f)*(1-t) + float64(b)*t
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		return int(v + 0.5)
	}
	return fmt.Sprintf("#%02x%02x%02x", mix(fr, br), mix(fg2, bg2), mix(fb, bb))
}

// parseHex 解析 "#rrggbb" 形式的颜色。
func parseHex(s string) (r, g, b int, ok bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff), true
}

// ---------- 提示音 ----------

// notifySoundCmd 返回播放提示音的命令。
//
// "bell" 直接让终端响一声（项目里已有响铃机制）；其余走内置合成音频。
// 播放是「尽力而为」：没有音频设备、被沙箱拦住都只是没声音，绝不能影响计时。
func (a *App) notifySoundCmd() tea.Cmd {
	switch a.cfg.EffectiveNotifySound() {
	case "":
		return nil
	case "bell":
		return func() tea.Msg { return bellMsg{} }
	default:
		name := a.cfg.EffectiveNotifySound()
		return func() tea.Msg {
			return soundPlayedMsg{name: name, err: playBuiltinSound(name)}
		}
	}
}

// bellMsg 让下一帧输出响铃字符。
type bellMsg struct{}

// soundPlayedMsg 报告提示音播放结果；失败只提示一次，不影响计时。
type soundPlayedMsg struct {
	name string
	err  error
}

// ---------- 推送文案 ----------

// notifyText 返回推送用的标题与正文。
func (a *App) notifyText(seg model.Segment) (title, body string) {
	label := a.timer.name
	if label == "" {
		label = "自由专注"
	}
	title = "KQFLOW · " + seg.Name
	if seg.Kind == model.SegmentKindBreak {
		body = "休息时间到了（" + label + "）"
	} else {
		body = "进入「" + seg.Name + "」，继续加油"
	}
	return title, body
}

// ---------- 设置项 ----------

// notifyPresetLabel 返回预设的展示名；空值或未知值回退到「关闭」。
func notifyPresetLabel(presets []struct {
	Key   string
	Label string
}, key string) string {
	if strings.TrimSpace(key) == "" {
		return "关闭"
	}
	for _, p := range presets {
		if p.Key == key {
			return p.Label
		}
	}
	return "关闭"
}

// nextPreset 返回列表里的下一个预设名（末项回到第一项）。
func nextPreset(presets []struct {
	Key   string
	Label string
}, current string) string {
	for i, p := range presets {
		if p.Key == current {
			return presets[(i+1)%len(presets)].Key
		}
	}
	// 未知值（例如手改成别的）从第一项重新开始。
	if len(presets) == 0 {
		return ""
	}
	return presets[0].Key
}

// cycleNotifyGlow 轮换流光预设并落盘。
func (a *App) cycleNotifyGlow() {
	a.cfg.NotifyGlow = nextPreset(notifyGlowPresets, a.cfg.NotifyGlow)
	a.saveConfig()
	a.setToast("流光提示："+notifyPresetLabel(notifyGlowPresets, a.cfg.NotifyGlow), toastInfo)
}

// cycleNotifySound 轮换提示音预设并落盘。
//
// 选到具体音效时顺手安排一次试听：否则用户要等到下次时段切换才知道自己选了什么。
// 试听通过 a.hearingCmd 交给设置页的按键处理回传（见 activateSetting）。
func (a *App) cycleNotifySound() {
	a.cfg.NotifySound = nextPreset(notifySoundPresets, a.cfg.NotifySound)
	a.saveConfig()
	a.setToast("提示音："+notifyPresetLabel(notifySoundPresets, a.cfg.NotifySound), toastInfo)
	if cmd := a.notifySoundCmd(); cmd != nil {
		a.hearingCmd = cmd
	}
}

// toggleNtfy 开关手机推送；开启时若还没有频道名就生成一个高熵频道。
//
// 频道名由程序生成而不是让用户起：ntfy 频道默认全网公开，靠用户的安全意识去
// 避免被猜到是不现实的（用户明确要求 CLI 替用户把关）。
func (a *App) toggleNtfy() {
	a.cfg.NtfyEnabled = !a.cfg.NtfyEnabled
	if !a.cfg.NtfyEnabled {
		a.saveConfig()
		a.setToast("手机推送已关闭", toastInfo)
		return
	}
	generated, isNew := a.cfg.EffectiveNtfyTopic()
	a.saveConfig()
	if generated == "" {
		a.setToast("生成频道名失败，推送未启用", toastErr)
		a.cfg.NtfyEnabled = false
		a.saveConfig()
		return
	}
	if isNew {
		a.setToast("已开启手机推送，并生成了一个随机频道", toastInfo)
		return
	}
	a.setToast("手机推送已开启", toastInfo)
}

// showNtfyHelp 打开一条说明：订阅地址、二维码、风险提示。
//
// 手机端不用手打频道名——扫描二维码或直接打开订阅地址即可。
func (a *App) showNtfyHelp() {
	if !a.cfg.NtfyReady() {
		a.setToast("先在上一项打开手机推送，程序会生成频道", toastWarn)
		return
	}
	a.ntfyHelp = true
}

// ---------- 手机推送说明页 ----------

// ntfyHelpContent 渲染「手机推送怎么用」的说明，只占中间栏。
//
// 返回 styled / plain 两份并交给 pageContent：说明文字 + 二维码很容易比中间栏高，
// 走 pageContent 才能自动裁剪并按 j/k 滚动。直接字符串拼接会把页脚顶出屏幕
// （这个 bug 在离屏预览里被看到过）。
func (a *App) ntfyHelpContent() (styled, plain []string) {
	inner := a.contentWidth()
	url := a.cfg.NtfyURL()

	add := func(s, p string) {
		styled = append(styled, s)
		plain = append(plain, p)
	}
	add(a.st.ModalTitle.Render(truncate("手机推送 / ntfy.sh", inner)), "手机推送 / ntfy.sh")
	add("", "")
	add(a.st.Text.Render(truncate("1. 手机安装 ntfy App（应用商店搜 ntfy）", inner)),
		"1. 手机安装 ntfy App（应用商店搜 ntfy）")
	add(a.st.Text.Render(truncate("2. 扫描下面的二维码，或直接打开这个地址订阅：", inner)),
		"2. 扫描下面的二维码，或直接打开这个地址订阅：")
	add("", "")
	for _, l := range wrapBalanced(url, max(8, inner-4)) {
		add(a.st.Accent.Render(truncate("   "+l, inner)), "   "+l)
	}
	add("", "")

	// 二维码：半块字符，一个字符表示竖向两个模块，省一半高度。
	if qr, err := renderQR(url, min(inner-6, 41)); err == nil && len(qr) > 0 {
		for _, l := range qr {
			add(a.st.Text.Render(truncate("   "+l, inner)), "   "+l)
		}
		add(a.st.Muted.Render(truncate("   （扫码后点 Subscribe 即可）", inner)), "   （扫码后点 Subscribe 即可）")
	} else {
		add(a.st.Muted.Render(truncate("   （二维码生成失败，请手动输入上面的地址）", inner)),
			"   （二维码生成失败，请手动输入上面的地址）")
	}
	add("", "")

	if config.NtfyTopicIsWeak(a.cfg.NtfyTopic) {
		add(a.st.Warn.Render(truncate("⚠ 当前频道名偏短，容易被猜到，建议重新生成", inner)),
			"⚠ 当前频道名偏短，容易被猜到，建议重新生成")
		add("", "")
	}

	add(a.st.Muted.Render(truncate("风险提示：", inner)), "风险提示：")
	for _, l := range wrapBalanced(config.NotifyDisclaimer, max(8, inner)) {
		add(a.st.Muted.Render(truncate(l, inner)), l)
	}
	add("", "")
	add(a.st.Muted.Render(truncate("j/k 滚动 · esc / q 返回", inner)), "j/k 滚动 · esc / q 返回")
	return styled, plain
}

// handleNtfyHelpKey 处理说明页的按键（支持滚动）。
func (a *App) handleNtfyHelpKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q", "?":
		a.ntfyHelp = false
		a.pageScroll = 0
		a.view = ViewSettings
	case "j", "down":
		a.pageScroll++
	case "k", "up":
		if a.pageScroll > 0 {
			a.pageScroll--
		}
	case "g", "home":
		a.pageScroll = 0
	case "ctrl+c":
		a.quitting = true
		return a, tea.Quit
	}
	return a, nil
}

// ---------- 小工具 ----------

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
