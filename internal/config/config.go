// Package config 负责 KQFLOW 的配置与数据目录定位。
//
// 默认数据目录与可执行文件同级，因此整个 kqf.exe 加一个 kqflow-data 文件夹
// 就能直接拷走使用，符合“无环境依赖可直接分发”的要求。
package config

import (
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
	// ShowNote 决定是否在看板上展示当日随手记的前几行。
	ShowNote bool `json:"show_note,omitempty"`
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
