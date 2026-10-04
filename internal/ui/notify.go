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
// 需求把提醒按「注意力距离」分成三档：人在屏幕前（流光）、人在设备附近但没看
// 屏幕（提示音）、人离开设备只带了手机（ntfy 推送）。三档各自可关，默认全关。
//
// 经过实机反馈后收敛为：
//   - 流光只做在 **Logo 那几行**（用户明确要求）——曾经铺满整个中间栏，很丑。
//   - 提示音只保留**终端响铃**（用户实测：程序合成的几种都放不出声，而系统响铃
//     好听且可用）。所以配置里它是个开/关，不再是预设列表。
//   - ntfy 推送保留（用户实测能正常收到短信），但**不再显示二维码**：
//     自研二维码在真机上始终扫不出来，改为直接给出订阅地址让用户复制。

// notifyDuration 是流光特效持续时长。
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
	return a.cfg.NotifyGlow || a.cfg.NotifySound || a.cfg.NtfyReady()
}

// notifyActive 报告此刻是否正在播放流光。
func (a *App) notifyActive() bool {
	if a.notifyState == nil || a.cfg == nil || !a.cfg.NotifyGlow {
		return false
	}
	return !a.notifyDone(a.clock.Now())
}

// notifyProgress 返回流光的播放进度（0..1）；没在播放时返回 1。
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

// ---------- 流光：只做在 Logo 上 ----------

// notifyColors 返回当前时段对应的两个配色端点。
func (a *App) notifyColors() (lipgloss.Color, lipgloss.Color) {
	if a.notifyState == nil {
		return a.st.Theme.Primary, a.st.Theme.Secondary
	}
	switch a.notifyState.kind {
	case model.SegmentKindBreak:
		return a.st.Theme.Warning, a.st.Theme.Secondary
	case "other":
		return a.st.Theme.Secondary, a.st.Theme.Primary
	default:
		return a.st.Theme.Success, a.st.Theme.Primary
	}
}

// notifyLogo 渲染提醒期间的 Logo：更亮的配色 + 更快的流动 + 脉冲高亮。
//
// 为什么做在 Logo 上：用户明确要求「只局限在 LOGO 所在的那几行」，而 Logo 本来
// 就有流动渐变，把它"点亮"是最自然、也最不割裂的做法（不用另画一层浮层，
// 左右面板与中间栏的其它内容都不受影响）。
func (a *App) notifyLogo(avail int) string {
	from, to := a.notifyColors()
	progress := a.notifyProgress(a.clock.Now())
	// 流动速度加快：正常是 animPhase 匀速推进，这里再叠一个来回摆动。
	phase := a.animPhase + progress*2.5
	logo := GradientLogo(from, to, phase, avail)
	if logo == "" {
		return ""
	}

	// 脉冲强度：两端淡入淡出（各占 25%），中间最亮。
	fade := 1.0
	switch {
	case progress < 0.25:
		fade = progress / 0.25
	case progress > 0.75:
		fade = (1 - progress) / 0.25
	}
	fade = quantizeFade(fade)

	lines := strings.Split(logo, "\n")
	pulseStyle := a.fadeStyle(to, fade*0.9)
	mark := pulseStyle.Render("◈")

	// 首尾各加一个记号：让"正在提醒"这件事在没有颜色差异的终端里也看得出来。
	out := make([]string, 0, len(lines))
	for i, l := range lines {
		pad := "  "
		if i == 0 || i == len(lines)-1 {
			pad = mark + " "
		}
		out = append(out, pad+l)
	}
	return strings.Join(out, "\n")
}

// ---------- 提示音：只保留终端响铃 ----------

// notifySoundCmd 返回播放提示音的命令。
//
// 只保留"终端响铃"：用户实测程序合成的几种音频在本机都放不出声（分别试过
// PowerShell SoundPlayer 与脱离进程的播放），而系统响铃好听且可用。所以这个
// 开关不再需要预设列表——要么响，要么不响。
//
// 响铃不直接写在这里，而是发一个消息让下一帧输出 `\a`：响铃属于渲染副作用，
// 不该在 Update 里直接动界面状态。
func (a *App) notifySoundCmd() tea.Cmd {
	if a.cfg == nil || !a.cfg.NotifySound {
		return nil
	}
	return func() tea.Msg { return bellMsg{} }
}

// bellMsg 让下一帧输出响铃字符。
type bellMsg struct{}

// ---------- 推送文案 ----------

// notifyText 返回推送用的标题与正文。
//
// 必须容忍**没有正在进行的计时**：设置页里的「测试」按钮随时可能按下，那时
// a.timer 是 nil（曾经因此 panic 过，被测试抓到）。
func (a *App) notifyText(seg model.Segment) (title, body string) {
	label := "自由专注"
	if a.timer != nil && a.timer.name != "" {
		label = a.timer.name
	}
	title = "KQFLOW · " + seg.Name
	if seg.Kind == model.SegmentKindBreak {
		body = "休息时间到了（" + label + "）"
	} else {
		body = "进入「" + seg.Name + "」，继续加油"
	}
	return title, body
}

// ---------- 手机推送说明页 ----------

// ntfyHelpContent 渲染「手机推送怎么用」的说明，只占中间栏。
//
// **不再有二维码**：自研的二维码在真机上始终扫不出来（两个版本都试过），
// 与其放一个扫不了的东西，不如老老实实把订阅地址给全，让用户复制。
// 地址很长，所以按行折好、并提示可以直接选中复制。
//
// 用户要求：看到地址就要看到风险声明，所以两者在同一页里紧挨着。
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
	add(a.st.Text.Render(truncate("2. 在 App 里点 Subscribe to topic，把下面这个地址的", inner)),
		"2. 在 App 里点 Subscribe to topic，把下面这个地址的")
	add(a.st.Text.Render(truncate("   最后一段（/ 后面那串）填进 topic，或直接用地址订阅。", inner)),
		"   最后一段（/ 后面那串）填进 topic，或直接用地址订阅。")
	add("", "")

	// 地址：分行给出，便于终端里选中复制。
	add(a.st.Muted.Render(truncate("订阅地址（可选中复制）：", inner)), "订阅地址（可选中复制）：")
	for _, l := range wrapBalanced(url, max(8, inner-2)) {
		add(a.st.Accent.Bold(true).Render(truncate("   "+l, inner)), "   "+l)
	}
	add("", "")
	add(a.st.Muted.Render(truncate("频道名（topic）：", inner)), "频道名（topic）：")
	add(a.st.Accent.Render(truncate("   "+a.cfg.NtfyTopic, inner)), "   "+a.cfg.NtfyTopic)
	add("", "")

	if config.NtfyTopicIsWeak(a.cfg.NtfyTopic) {
		add(a.st.Warn.Render(truncate("⚠ 当前频道名偏短，容易被猜到，建议在设置里重新生成", inner)),
			"⚠ 当前频道名偏短，容易被猜到，建议在设置里重新生成")
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

// ---------- 设置项 ----------

// cycleNotifyGlow 开关流光。
func (a *App) cycleNotifyGlow() {
	a.cfg.NotifyGlow = !a.cfg.NotifyGlow
	a.saveConfig()
	a.setToast("流光提示："+onOff(a.cfg.NotifyGlow), toastInfo)
}

// cycleNotifySound 开关提示音（只保留终端响铃）。
func (a *App) cycleNotifySound() {
	a.cfg.NotifySound = !a.cfg.NotifySound
	a.saveConfig()
	a.setToast("提示音（系统响铃）："+onOff(a.cfg.NotifySound), toastInfo)
	// 开启时立刻响一声，让用户确认真的能响。
	if cmd := a.notifySoundCmd(); cmd != nil {
		a.hearingCmd = cmd
	}
}

// onOff 把布尔值写成中文开关文案。
func onOff(v bool) string {
	if v {
		return "开"
	}
	return "关"
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
		a.setToast("已开启手机推送并生成随机频道；订阅地址已显示在设置页", toastInfo)
		return
	}
	a.setToast("手机推送已开启", toastInfo)
}

// regenerateNtfyTopic 换一个手机频道（用户明确要求：不要求就不变，要求才换）。
//
// 换频道会让手机上的旧订阅失效，所以必须把后果说清楚。
func (a *App) regenerateNtfyTopic() {
	if !a.cfg.NtfyReady() {
		a.setToast("先打开上一项「手机推送」，程序会先生成一个频道", toastWarn)
		return
	}
	old := a.cfg.NtfyTopic
	topic, err := config.GenerateNtfyTopic()
	if err != nil {
		a.setToast("生成新频道失败："+err.Error(), toastErr)
		return
	}
	if topic == old {
		// 概率极低（256 位熵），真出现就再取一次。
		a.setToast("生成重复频道，请再试一次", toastWarn)
		return
	}
	a.cfg.NtfyTopic = topic
	a.cfg.NtfyEnabled = true
	a.saveConfig()
	a.setToast("已换新频道：手机需要重新订阅一次（旧频道已失效）", toastInfo)
	// 换完直接打开说明页，用户不用再找入口就能复制新地址。
	a.showNtfyHelp()
}

// showNtfyHelp 打开说明页（订阅地址 + 风险提示）。
func (a *App) showNtfyHelp() {
	if !a.cfg.NtfyReady() {
		a.setToast("先在上一项打开手机推送，程序会生成频道", toastWarn)
		return
	}
	a.ntfyHelp = true
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

// toggleAutoArchive 开关「专注结束时自动结束并归档」。
func (a *App) toggleAutoArchive() {
	a.cfg.AutoArchiveOnFinish = !a.cfg.AutoArchiveOnFinish
	a.saveConfig()
	if a.cfg.AutoArchiveOnFinish {
		a.setToast("专注走完会立刻结束并归档，不用再确认", toastInfo)
		return
	}
	a.setToast("专注走完会停住等你确认（按 p 菜单结束）", toastInfo)
}

// demoNotify 立刻演示一次提醒：流光 + 提示音 + 手机推送。
//
// 用户要求加一个测试功能，理由是前三项都得等到"时段切换"才能看到效果，验证
// 成本太高。这里就地把三种提醒都触发一次，让用户马上知道哪一项没生效。
//
// 每一项独立判断：只打开了的才会执行，并在提示里说清这次跑了哪几项、哪几项
// 因为没开而跳过——否则"点了没反应"用户无从判断。
func (a *App) demoNotify() {
	if !a.notifyEnabled() {
		a.setToast("三项提醒都没打开，先在设置里打开至少一项", toastWarn)
		return
	}

	now := a.clock.Now()
	demoSeg := model.Segment{Name: "测试提醒", Kind: model.SegmentKindFocus}
	a.notifyState = &notifyState{
		started: now,
		kind:    demoSeg.Kind,
		toName:  "测试提醒",
		pushCmd: a.ntfyPushCmd(demoSeg),
	}

	ran := []string{}
	cmds := []tea.Cmd{animCmd()}
	if a.cfg.NotifyGlow {
		ran = append(ran, "流光")
	} else {
		ran = append(ran, "流光（未开）")
	}
	if sc := a.notifySoundCmd(); sc != nil {
		ran = append(ran, "提示音")
		cmds = append(cmds, sc)
	} else {
		ran = append(ran, "提示音（未开）")
	}
	if a.notifyState.pushCmd != nil {
		ran = append(ran, "手机推送")
		cmds = append(cmds, a.notifyState.pushCmd)
	} else {
		ran = append(ran, "手机推送（未开）")
	}

	a.setToast("测试："+strings.Join(ran, " · "), toastInfo)
	// 命令通过 hearingCmd 交给设置页的按键处理回传（与试听同一机制）。
	a.hearingCmd = tea.Batch(cmds...)
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
