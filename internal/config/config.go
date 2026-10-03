// Package config 负责 Kairos 的配置与数据目录定位。
//
// 默认数据目录与可执行文件同级，因此整个 kair.exe 加一个 kairos-data 文件夹
// 就能直接拷走使用，符合“无环境依赖可直接分发”的要求。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// FileName 是配置文件名。
const FileName = "config.json"

// Config 是 Kairos 的全部可配置项。
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

// Paths 汇总 Kairos 用到的所有路径。
type Paths struct {
	// Root 是数据根目录。
	Root string
	// ConfigFile 是配置文件路径。
	ConfigFile string
}

// Dir 返回数据根目录：优先 KAIROS_HOME 环境变量，其次配置里的 DataDir，
// 最后是可执行文件同级的 kairos-data。
func Dir(cfg *Config) (string, error) {
	if env := os.Getenv("KAIROS_HOME"); env != "" {
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
		return filepath.Join(filepath.Dir(exe), "kairos-data"), nil
	}
	// 无法定位可执行文件时退回用户主目录，保证程序仍可运行。
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法确定数据目录: %w", err)
	}
	return filepath.Join(home, ".kairos"), nil
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
	normalize(cfg)
	return cfg, nil
}

// Save 原子地写出配置。
func Save(p *Paths, cfg *Config) error {
	if cfg.SchemaVersion == 0 {
		cfg.SchemaVersion = 1
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
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("替换 %s 失败: %w", path, err)
	}
	return nil
}

// AtomicWrite 导出原子写入，供存储层复用。
func AtomicWrite(path string, data []byte) error { return atomicWrite(path, data) }
