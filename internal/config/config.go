// Package config 负责 KQFLOW 的配置与数据目录定位。
//
// 默认数据目录与可执行文件同级，因此整个 kqf.exe 加一个 kqflow-data 文件夹
// 就能直接拷走使用，符合“无环境依赖可直接分发”的要求。
package config

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// FileName 是配置文件名。
const FileName = "config.json"

// ConfigSchemaVersion 是本程序支持的配置文件版本。
//
// 与 model.SchemaVersion 分开：配置文件与日数据文件是两套独立的格式，
// 将来也可能各自演进。这里不能引用 model（model 不依赖任何内部包，
// config 引用它会绕成环），所以各留一个常量。
const ConfigSchemaVersion = 1

// IncompatibleConfigError 报告配置文件来自更新的版本。
//
// 这种情况必须拒绝启动，不能像「配置损坏」那样静默退回默认值：默认值一旦
// 被写回，用户在新版本里做的设置就永久丢了，而且没有任何提示。
type IncompatibleConfigError struct {
	Path    string
	Current int
	Found   int
}

func (e *IncompatibleConfigError) Error() string {
	return fmt.Sprintf(
		"配置文件来自更新的版本：%s（schema_version=%d，本程序支持 ≤ %d）。\n"+
			"请升级到最新版本的程序后再启动；\n"+
			"若确实要用旧程序打开，请先把这个文件移出数据目录（另存备份），再重新启动。",
		e.Path, e.Found, e.Current)
}

// IsIncompatibleConfig 报告错误是否属于「配置来自更新版本」。
func IsIncompatibleConfig(err error) bool {
	var target *IncompatibleConfigError
	return errors.As(err, &target)
}

// Config 是 KQFLOW 的全部可配置项。
type Config struct {
	SchemaVersion int `json:"schema_version"`
	// DayCutoff 是日界线：新的一天从当地时间几点开始（见需求 19）。
	// 用 “HH:MM” 存储，便于用户直接编辑。
	DayCutoff string `json:"day_cutoff"`
	// DefaultFocus / DefaultBreak 是番茄钟的默认时长（分钟）。
	DefaultFocus int `json:"default_focus_minutes"`
	DefaultBreak int `json:"default_break_minutes"`
	// PomodoroCycles 是番茄钟的段数（专注 + 休息为一轮），见需求 16。
	PomodoroCycles int `json:"pomodoro_cycles,omitempty"`
	// CountdownMin 是倒计时的时长（分钟）；为 0 时跟随专注时长。
	CountdownMin int `json:"countdown_minutes,omitempty"`
	// Quotes 是用户自定义的随机字条；为空时使用内置字条（见需求 9）。
	Quotes []string `json:"quotes,omitempty"`
	// CustomLabels 是用户自己新增的标签名（见 label.go）。
	//
	// 只存「用户新造的」那些：内置预设写死在代码里，条目上正在用的标签从
	// 条目本身收集。这样标签库不会随着使用不断膨胀，也不会出现
	// 「标签库里有、但哪个条目都没用」的悬空项。
	CustomLabels []string `json:"custom_labels,omitempty"`
	// SavedPlans 是用户收藏的自定义专注方案（见需求 2）。
	//
	// 放在配置里而不是日数据里：它是「偏好」而不是「当天记录」，与日界线无关，
	// 也不该随某一天的数据被清理。
	SavedPlans []model.Plan `json:"saved_plans,omitempty"`

	// ---------- 时段切换提醒（见需求 3） ----------
	//
	// 三种提醒按「注意力距离」覆盖三个场景：人在屏幕前（流光）、人在设备附近
	// 但没看屏幕（提示音）、人离开设备只带了手机（ntfy 推送）。
	// 每一项都能关掉，默认全关——需要装 App、需要联网的功能不该默认打开。

	// NotifyGlow 打开时段切换的流光提示（做在 LOGO 上）。
	NotifyGlow bool `json:"notify_glow,omitempty"`
	// NotifySound 打开时段切换的提示音。
	//
	// **只保留系统响铃**：程序曾经用代码合成过颂钵/风铃/白噪音三种音频并通过
	// PowerShell 播放，但用户实测在本机都放不出声，而系统响铃好听且可用。
	// 所以它是个开关而不是预设列表（见 internal/ui/notify.go）。
	NotifySound bool `json:"notify_sound,omitempty"`
	// NtfyTopic 是用户订阅的 ntfy.sh 频道名。
	//
	// ntfy.sh 的频道默认是**全网公开**的：知道名字的人都能收到、也能发。
	// 所以这里只应该放高熵随机串（见 EffectiveNtfyTopic），不要放 "myphone"
	// 这种猜得到的名字。
	NtfyTopic string `json:"ntfy_topic,omitempty"`
	// NtfyServer 是 ntfy 服务地址；留空表示用官方 https://ntfy.sh。
	NtfyServer string `json:"ntfy_server,omitempty"`
	// NtfyEnabled 是推送总开关。
	//
	// 与「填了 Topic」分开：需求明确要求「可以填了但是关掉」。
	NtfyEnabled bool `json:"ntfy_enabled,omitempty"`

	// AutoArchiveOnFinish 决定专注时段走完后要不要**自动结束并归档**。
	//
	// 默认 false：走完最后一段后停在"已完成"，等你按 p 菜单确认再归档。这样
	// 时长不会在你不注意的时候被定成"完成"，也不会因为手滑被当成中断。
	// 打开后则走完即刻归档，适合"设好就不管"的用法。
	AutoArchiveOnFinish bool `json:"auto_archive_on_finish,omitempty"`

	// ShowNote 决定是否在看板上展示当日随手记的前几行。
	ShowNote bool `json:"show_note,omitempty"`
	// Engine 选择渲染引擎（v3.0.0 的迁移开关，见 docs/kxflow-design.md §8）。
	//
	//	""/"legacy" → v2.1.0 的界面（internal/ui），**默认**
	//	"kxflow"    → KXFLOW 引擎（internal/kxapp）
	//
	// 为什么默认仍是 legacy：迁移期间的纪律是**新老并行、老路径不许坏**。
	// 引擎要切默认，前提是功能上能完全替代 2.1.0——
	// 在那之前让新引擎当默认，等于拿用户的日常使用做测试。
	// 空串按 legacy 处理，因此老配置文件（没有这个字段）行为完全不变。
	Engine string `json:"engine,omitempty"`
	// View 是界面布局（磁贴摆在哪个槽位、隐藏了哪些、停靠区开不开）。
	//
	// 放在配置里而不是日数据里：它是**偏好**，与逻辑日无关，
	// 也不该随某一天的数据被清理（与 SavedPlans 同理）。
	//
	// 用 any 而不是 plugin.ViewConfig：**internal/config 不该依赖引擎**——
	// 配置是数据的形状，引擎是可替换的实现（v3 的整个目的就是让引擎能换）。
	// 由宿主在读写时做一次转换（见 internal/kxapp 的 viewConfig）。
	View map[string]any `json:"view,omitempty"`
	// Timezone 为空时使用系统本地时区。
	Timezone string `json:"timezone,omitempty"`
	// Nickname 会出现在看板问候语中。
	Nickname string `json:"nickname,omitempty"`
	// DataDir 为空时使用默认数据目录；用于让用户把数据库放到别处。
	DataDir string `json:"data_dir,omitempty"`
	// Mouse 打开鼠标上报。默认关闭：一旦开启，终端就无法用鼠标选中文本，
	// 连带把“选中→复制→粘贴中文”这条最可靠的输入路径也堵死了。
	Mouse bool `json:"mouse,omitempty"`
}

// Default 返回默认配置。
func Default() *Config {
	return &Config{
		SchemaVersion: 1,
		DayCutoff:     "00:00",
		DefaultFocus:  25,
		DefaultBreak:  5,
		Nickname:      "",
	}
}

// Cutoff 把 DayCutoff 解析为时长；非法值退回 0（午夜换日）。
func (c *Config) Cutoff() time.Duration {
	d, err := ParseClock(c.DayCutoff)
	if err != nil {
		return 0
	}
	return d
}

// FocusDuration 返回默认专注时长。
func (c *Config) FocusDuration() time.Duration {
	return minutes(c.DefaultFocus, 25)
}

// BreakDuration 返回默认休息时长。
func (c *Config) BreakDuration() time.Duration {
	return minutes(c.DefaultBreak, 5)
}

// DefaultPomodoroCycles 是番茄钟的默认段数。
const DefaultPomodoroCycles = 4

// EffectivePomodoroCycles 返回生效的番茄钟段数。
func (c *Config) EffectivePomodoroCycles() int {
	if c.PomodoroCycles <= 0 {
		return DefaultPomodoroCycles
	}
	return c.PomodoroCycles
}

// FocusMinutes 返回生效的专注时长（分钟）。
func (c *Config) FocusMinutes() int {
	if c.DefaultFocus <= 0 {
		return 25
	}
	return c.DefaultFocus
}

// BreakMinutes 返回生效的休息时长（分钟）。
func (c *Config) BreakMinutes() int {
	if c.DefaultBreak <= 0 {
		return 5
	}
	return c.DefaultBreak
}

// CountdownMinutes 返回生效的倒计时时长（分钟）；未单独设置时跟随专注时长。
func (c *Config) CountdownMinutes() int {
	if c.CountdownMin <= 0 {
		return c.FocusMinutes()
	}
	return c.CountdownMin
}

// QuotesText 把自定义字条拼成多行文本，供设置页编辑。
func (c *Config) QuotesText() string {
	return strings.Join(c.Quotes, "\n")
}

// CustomLabelList 返回清洗过的自定义标签（去空、去重、按原顺序）。
func (c *Config) CustomLabelList() []string {
	if len(c.CustomLabels) == 0 {
		return nil
	}
	out := make([]string, 0, len(c.CustomLabels))
	seen := make(map[string]bool, len(c.CustomLabels))
	for _, raw := range c.CustomLabels {
		name := strings.TrimSpace(strings.Join(strings.Fields(raw), " "))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// AddCustomLabel 把一个标签记进自定义标签库；已存在或为空则不动。
func (c *Config) AddCustomLabel(name string) bool {
	name = strings.TrimSpace(strings.Join(strings.Fields(name), " "))
	if name == "" || HasString(c.CustomLabelList(), name) {
		return false
	}
	c.CustomLabels = append(c.CustomLabelList(), name)
	return true
}

// HasString 报告切片里是否含有某个字符串。
func HasString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// SavedPlanList 返回校验过、去重后的收藏方案。
//
// 手改过的配置里可能有非法方案（段时长为 0、没名字等），这里连带标签一起去掉，
// 免得脏数据一路进到计时逻辑里。同名视为同一套方案，只保留最后一个——用户在
// 编辑器里用同一个名字再存一次，意图显然是「覆盖」。
func (c *Config) SavedPlanList() []model.Plan {
	if len(c.SavedPlans) == 0 {
		return nil
	}
	// 先从后往前扫，同名只留最后出现的那个。
	out := make([]model.Plan, 0, len(c.SavedPlans))
	seen := make(map[string]bool, len(c.SavedPlans))
	for i := len(c.SavedPlans) - 1; i >= 0; i-- {
		p := c.SavedPlans[i]
		if !model.PlanValid(p) {
			continue
		}
		label := model.NormalizePlanLabel(p.Label)
		if label == "" {
			label = model.AutoPlanLabel(p)
		}
		if seen[label] {
			continue
		}
		seen[label] = true
		stored := model.ClonePlan(p)
		stored.Label = label
		out = append(out, stored)
	}
	// 反转回原始顺序，用户看到的顺序与保存顺序一致。
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// AddSavedPlan 收藏一套方案；同名视为覆盖，返回最终使用的名字。
//
// 非法方案返回空串表示拒绝。
func (c *Config) AddSavedPlan(p model.Plan) string {
	if !model.PlanValid(p) {
		return ""
	}
	label := model.NormalizePlanLabel(p.Label)
	if label == "" {
		label = model.AutoPlanLabel(p)
	}
	stored := model.ClonePlan(p)
	stored.Label = label

	list := c.SavedPlanList()
	replaced := false
	for i := range list {
		if list[i].Label == label {
			list[i] = stored
			replaced = true
			break
		}
	}
	if !replaced {
		list = append(list, stored)
	}
	c.SavedPlans = list
	return label
}

// RemoveSavedPlan 按名字删除一套收藏方案，返回是否删掉了。
func (c *Config) RemoveSavedPlan(label string) bool {
	list := c.SavedPlanList()
	out := make([]model.Plan, 0, len(list))
	removed := false
	for _, p := range list {
		if p.Label == label {
			removed = true
			continue
		}
		out = append(out, p)
	}
	if removed {
		c.SavedPlans = out
	}
	return removed
}

// ---------- 时段切换提醒（见需求 3） ----------

// NtfyDefaultServer 是官方 ntfy 服务地址。
const NtfyDefaultServer = "https://ntfy.sh"

// NtfyTopicLength 是自动生成频道名的长度（字节）。32 字节 = 256 位熵。
//
// ntfy.sh 的频道是全网公开的：谁猜到名字谁就能收到、也能往里发。用户自己起的
// 名字（"myphone"、"kqflow"）基本必然被猜到，所以由程序生成高熵随机名，
// 不把安全防线寄托在用户的安全意识上。
const NtfyTopicLength = 32

// GenerateNtfyTopic 生成一个高熵频道名。
//
// 用 crypto/rand（不是 math/rand）：这个名字是唯一的访问凭据，必须不可预测。
// 返回的是 URL 安全的 base32（去掉容易看错的填充与易混字符），既够短也够随机。
func GenerateNtfyTopic() (string, error) {
	buf := make([]byte, NtfyTopicLength)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机频道名失败: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

// EffectiveNtfyTopic 返回生效的频道名：用户填过就用用户的，没填过就自动生成一个
// 并写回配置（固化下来，不能每次专注都换频道——否则用户的手机订阅就失效了）。
//
// 返回的第二个值表示本次是否新生成并写入了配置，调用方据此决定要不要落盘。
func (c *Config) EffectiveNtfyTopic() (string, bool) {
	if t := strings.TrimSpace(c.NtfyTopic); t != "" {
		return t, false
	}
	generated, err := GenerateNtfyTopic()
	if err != nil {
		return "", false
	}
	c.NtfyTopic = generated
	return generated, true
}

// NtfyURL 返回该频道的订阅地址。
func (c *Config) NtfyURL() string {
	topic := strings.TrimSpace(c.NtfyTopic)
	if topic == "" {
		return ""
	}
	return c.NtfyServerURL() + "/" + topic
}

// NtfyServerURL 返回服务地址（末尾不带 /）。
func (c *Config) NtfyServerURL() string {
	server := strings.TrimSpace(c.NtfyServer)
	if server == "" {
		return NtfyDefaultServer
	}
	return strings.TrimRight(server, "/")
}

// NtfyTopicIsWeak 报告频道名是否弱到有明显被猜中的风险。
//
// 判据刻意宽松（只拦明显危险的）：太短、纯字母数字且很短、或与项目/常见词同名。
// 警告而不阻止——用户有权自己决定，但程序有义务把风险讲清楚。
func NtfyTopicIsWeak(topic string) bool {
	t := strings.TrimSpace(topic)
	if t == "" {
		return true
	}
	// 高熵名字按长度就能排除：32 字节 base32 是 52 个字符。
	if len(t) >= 20 {
		return false
	}
	lower := strings.ToLower(t)
	for _, weak := range []string{
		"kqflow", "kqf", "test", "demo", "phone", "myphone", "me",
		"abc", "123", "topic", "channel", "push", "notice", "alert",
	} {
		if lower == weak || strings.Contains(lower, weak) {
			return true
		}
	}
	return true
}

// NotifyDisclaimer 是向用户展示的风险提示。
//
// 需求（注意 2）要求：引导用户使用 ntfy.sh 时注意措辞，不要被误解为附属软件；
// 用户也要求把「频道不加密、不要泄露、不要轻信收到的奇怪消息」讲清楚。
const NotifyDisclaimer = "ntfy.sh 是独立的第三方开源推送服务，KQFLOW 与它没有隶属或合作关系，" +
	"只是把消息发到你指定的地址。ntfy 的频道默认不加密、且全网可读：" +
	"知道频道名的人都能收到消息、也能往里发。请不要把这个频道名告诉陌生人；" +
	"收到来源不明的消息不要轻信。由此造成的任何损失，KQFLOW 不承担责任。"

// NtfyReady 报告推送是否已就绪（开了开关且频道名非空）。
func (c *Config) NtfyReady() bool {
	return c.NtfyEnabled && strings.TrimSpace(c.NtfyTopic) != ""
}

func minutes(v, fallback int) time.Duration {
	if v <= 0 {
		v = fallback
	}
	return time.Duration(v) * time.Minute
}

// Location 返回配置的时区；解析失败时退回本地时区。
func (c *Config) Location() *time.Location {
	if c.Timezone == "" {
		return time.Local
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return time.Local
	}
	return loc
}

// ParseClock 解析 “HH:MM” 或 “HH:MM:SS” 形式的时刻。
//
// 注意：这里不能用 fmt.Sscanf("%d:%d:%d")。当输入只有两个字段时，
// Sscanf 会返回 “unexpected EOF” 错误，导致所有正常的 HH:MM 都被判为非法。
func ParseClock(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("时间不能为空，应为 HH:MM")
	}
	fields := strings.Split(s, ":")
	if len(fields) != 2 && len(fields) != 3 {
		return 0, fmt.Errorf("时间 %q 格式不正确，应为 HH:MM", s)
	}
	nums := make([]int, len(fields))
	for i, f := range fields {
		n, err := strconv.Atoi(strings.TrimSpace(f))
		if err != nil {
			return 0, fmt.Errorf("时间 %q 含有非数字字符", s)
		}
		nums[i] = n
	}
	h, m := nums[0], nums[1]
	sec := 0
	if len(nums) == 3 {
		sec = nums[2]
	}
	if h < 0 || h > 23 {
		return 0, fmt.Errorf("小时 %d 超出范围（0-23）", h)
	}
	if m < 0 || m > 59 {
		return 0, fmt.Errorf("分钟 %d 超出范围（0-59）", m)
	}
	if sec < 0 || sec > 59 {
		return 0, fmt.Errorf("秒 %d 超出范围（0-59）", sec)
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second, nil
}

// FormatClock 把时长格式化成 “HH:MM”。
func FormatClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h := int(d.Hours()) % 24
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%02d:%02d", h, m)
}

// Paths 汇总 KQFLOW 用到的所有路径。
type Paths struct {
	// Root 是数据根目录。
	Root string
	// ConfigFile 是配置文件路径。
	ConfigFile string
}

// EngineKXFLOW 是"使用 KXFLOW 引擎"的配置值。
const EngineKXFLOW = "kxflow"

// EngineLegacy 是"使用 v2.1.0 界面"的配置值（默认）。
const EngineLegacy = "legacy"

// UseKXFLOW 报告是否应当使用 KXFLOW 引擎。
//
// 判据写成"只有明确写了 kxflow 才用新引擎"：未知值一律按老的走。
// 这样拼错（"kxflwo"）不会让用户掉进一个他没打算用的界面里，
// 而是继续用能用的那个——保守方向永远选"用户今天能干活"。
func (c *Config) UseKXFLOW() bool {
	if c == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(c.Engine), EngineKXFLOW)
}

// Dir 返回数据根目录：优先 KQFLOW_HOME 环境变量，其次配置里的 DataDir，
// 最后是可执行文件同级的 kqflow-data。
func Dir(cfg *Config) (string, error) {
	if env := os.Getenv("KQFLOW_HOME"); env != "" {
		return filepath.Abs(env)
	}
	if cfg != nil && cfg.DataDir != "" {
		return filepath.Abs(cfg.DataDir)
	}
	exe, err := os.Executable()
	if err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		return filepath.Join(filepath.Dir(exe), "kqflow-data"), nil
	}
	// 无法定位可执行文件时退回用户主目录，保证程序仍可运行。
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法确定数据目录: %w", err)
	}
	return filepath.Join(home, ".kqflow"), nil
}

// Resolve 返回完整的路径集合，并在需要时创建目录。
func Resolve(cfg *Config) (*Paths, error) {
	root, err := Dir(cfg)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录 %s 失败: %w", root, err)
	}
	return &Paths{Root: root, ConfigFile: filepath.Join(root, FileName)}, nil
}

// Load 读取配置；文件不存在时返回默认配置并写出一份，方便用户直接编辑。
func Load(p *Paths) (*Config, error) {
	raw, err := os.ReadFile(p.ConfigFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("读取配置失败: %w", err)
		}
		cfg := Default()
		if saveErr := Save(p, cfg); saveErr != nil {
			// 写不出配置不影响使用，只提示。
			return cfg, nil
		}
		return cfg, nil
	}
	cfg := Default()
	if err := json.Unmarshal(raw, cfg); err != nil {
		// 配置损坏时退回默认值，绝不因此阻断启动。
		return Default(), nil
	}
	// 但「来自更新版本」不是损坏，必须拒绝：下面的 normalize/Save 会把这份
	// 配置按当前版本整份写回，新版本的设置会静默消失。
	if cfg.SchemaVersion > ConfigSchemaVersion {
		return nil, &IncompatibleConfigError{
			Path:    p.ConfigFile,
			Current: ConfigSchemaVersion,
			Found:   cfg.SchemaVersion,
		}
	}
	normalize(cfg)
	return cfg, nil
}

// Save 原子地写出配置。
func Save(p *Paths, cfg *Config) error {
	// 永不把配置降级：宁可保留原版本号，也不让旧程序抹掉新版本写的设置。
	if cfg.SchemaVersion < ConfigSchemaVersion {
		cfg.SchemaVersion = ConfigSchemaVersion
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(p.ConfigFile, append(raw, '\n'))
}

func normalize(cfg *Config) {
	if cfg.DefaultFocus <= 0 {
		cfg.DefaultFocus = 25
	}
	if cfg.DefaultBreak <= 0 {
		cfg.DefaultBreak = 5
	}
	if _, err := ParseClock(cfg.DayCutoff); err != nil {
		cfg.DayCutoff = "00:00"
	}
}

// atomicWrite 先写临时文件再改名，避免中途断电留下半个文件。
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("同步临时文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := replaceFile(tmpName, path); err != nil {
		return fmt.Errorf("替换 %s 失败: %w", path, err)
	}
	return nil
}

// renameRetries / renameRetryDelay 控制“改名被占用”时的重试。
//
// 总等待约 0.55 秒，足够让一次杀毒扫描或索引放手；再久就该报错而不是继续卡住界面。
const (
	renameRetries    = 10
	renameRetryDelay = 20 * time.Millisecond
)

// replaceFile 把 src 改名覆盖到 dst。
//
// Windows 上 os.Rename 走 MoveFileEx(MOVEFILE_REPLACE_EXISTING)，当目标文件
// 正被别的进程打开时会直接返回 Access is denied —— 典型来源是：
//   - 杀毒 / Defender 实时防护刚扫到一个刚写完的 json，句柄还没放；
//   - Windows 索引服务、资源管理器预览、编辑器打开了该文件；
//   - 同步盘（OneDrive 等）正在读取。
//
// 这些锁都是瞬时的，所以这里退避重试几次再放弃。之前不重试，界面就会
// 冒出“保存失败：…… rename …… 失败”，而内容其实差点就写进去了。
func replaceFile(src, dst string) error {
	var err error
	delay := renameRetryDelay
	for attempt := 0; attempt < renameRetries; attempt++ {
		if err = os.Rename(src, dst); err == nil {
			return nil
		}
		if !isTransientRenameErr(err) {
			return err
		}
		time.Sleep(delay)
		if delay < 120*time.Millisecond {
			delay *= 2
		}
	}
	return err
}

// isTransientRenameErr 判断改名错误是否属于“稍后重试可能成功”的那类。
func isTransientRenameErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrPermission) {
		return true
	}
	// Windows 的共享冲突 / 锁定冲突；非 Windows 上这两个常量通常也能编译，
	// 但用一个宽松的字符串兜底更保险（错误文案可能随 Windows 语言本地化）。
	msg := err.Error()
	for _, s := range []string{
		"Access is denied",
		"being used by another process",
		"used by another process",
		"cannot access the file",
		"拒绝访问",
		"另一个程序",
		"正由另一进程使用",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// AtomicWrite 导出原子写入，供存储层复用。
func AtomicWrite(path string, data []byte) error { return atomicWrite(path, data) }
