package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestParseClock 验证时刻解析。
//
// 这里专门覆盖 “HH:MM” 两字段形式：早期实现用 fmt.Sscanf("%d:%d:%d") 解析，
// 两字段输入会返回 unexpected EOF，导致所有日界线设置被静默重置为 00:00。
func TestParseClock(t *testing.T) {
	ok := map[string]time.Duration{
		"00:00":    0,
		"04:00":    4 * time.Hour,
		"4:00":     4 * time.Hour,
		"23:59":    23*time.Hour + 59*time.Minute,
		"12:30:15": 12*time.Hour + 30*time.Minute + 15*time.Second,
		" 06:45 ":  6*time.Hour + 45*time.Minute,
		"0:00":     0,
	}
	for in, want := range ok {
		got, err := ParseClock(in)
		if err != nil {
			t.Errorf("ParseClock(%q) 不应报错: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseClock(%q) = %v，期望 %v", in, got, want)
		}
	}

	bad := []string{"", "abc", "04", "04:00:00:00", "24:00", "04:60", "04:-1", "12:aa", "04:00:99"}
	for _, in := range bad {
		if _, err := ParseClock(in); err == nil {
			t.Errorf("ParseClock(%q) 应报错", in)
		}
	}
}

// TestFormatClock 验证时刻格式化。
func TestFormatClock(t *testing.T) {
	cases := map[time.Duration]string{
		0:                             "00:00",
		4 * time.Hour:                 "04:00",
		23*time.Hour + 59*time.Minute: "23:59",
		25 * time.Hour:                "01:00",
		-1:                            "00:00",
	}
	for d, want := range cases {
		if got := FormatClock(d); got != want {
			t.Errorf("FormatClock(%v) = %s，期望 %s", d, got, want)
		}
	}
}

// TestCutoffRoundTrip 验证日界线写入配置后能原样读回。
func TestCutoffRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := &Paths{Root: dir, ConfigFile: filepath.Join(dir, FileName)}

	cfg := Default()
	cfg.DayCutoff = "04:00"
	cfg.Nickname = "Kevin"
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Cutoff(); got != 4*time.Hour {
		t.Fatalf("日界线应为 4h，实际 %v", got)
	}

	loaded, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DayCutoff != "04:00" {
		t.Errorf("读回的日界线应为 04:00，实际 %q", loaded.DayCutoff)
	}
	if loaded.Cutoff() != 4*time.Hour {
		t.Errorf("读回的日界线时长应为 4h，实际 %v", loaded.Cutoff())
	}
	if loaded.Nickname != "Kevin" {
		t.Errorf("昵称应为 Kevin，实际 %q", loaded.Nickname)
	}
}

// TestLoadCreatesDefaultConfig 验证首次运行会写出默认配置。
func TestLoadCreatesDefaultConfig(t *testing.T) {
	dir := t.TempDir()
	p := &Paths{Root: dir, ConfigFile: filepath.Join(dir, FileName)}

	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DayCutoff != "00:00" {
		t.Errorf("默认日界线应为 00:00，实际 %q", cfg.DayCutoff)
	}
	if _, err := os.Stat(p.ConfigFile); err != nil {
		t.Errorf("首次运行应写出配置文件: %v", err)
	}
}

// TestLoadCorruptConfigFallsBack 验证配置损坏时不影响启动。
func TestLoadCorruptConfigFallsBack(t *testing.T) {
	dir := t.TempDir()
	p := &Paths{Root: dir, ConfigFile: filepath.Join(dir, FileName)}
	if err := os.WriteFile(p.ConfigFile, []byte("{ 坏掉的配置"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("配置损坏时不应阻断启动: %v", err)
	}
	if cfg.DayCutoff != "00:00" {
		t.Errorf("应退回默认配置，实际日界线 %q", cfg.DayCutoff)
	}
}

// TestNormalizeFixesInvalidCutoff 验证非法日界线会被纠正。
func TestNormalizeFixesInvalidCutoff(t *testing.T) {
	cfg := Default()
	cfg.DayCutoff = "25:99"
	normalize(cfg)
	if cfg.DayCutoff != "00:00" {
		t.Errorf("非法日界线应被纠正为 00:00，实际 %q", cfg.DayCutoff)
	}
}

// TestDurationHelpers 验证默认时长兜底。
func TestDurationHelpers(t *testing.T) {
	cfg := Default()
	if cfg.FocusDuration() != 25*time.Minute {
		t.Errorf("默认专注时长应为 25m，实际 %v", cfg.FocusDuration())
	}
	if cfg.BreakDuration() != 5*time.Minute {
		t.Errorf("默认休息时长应为 5m，实际 %v", cfg.BreakDuration())
	}
	cfg.DefaultFocus = 0
	if cfg.FocusDuration() != 25*time.Minute {
		t.Errorf("非法专注时长应退回 25m，实际 %v", cfg.FocusDuration())
	}
	cfg.DefaultBreak = -3
	if cfg.BreakDuration() != 5*time.Minute {
		t.Errorf("非法休息时长应退回 5m，实际 %v", cfg.BreakDuration())
	}
}

// TestLocationFallsBackToLocal 验证时区解析失败时退回本地时区。
func TestLocationFallsBackToLocal(t *testing.T) {
	cfg := Default()
	if cfg.Location() != time.Local {
		t.Error("空时区应使用本地时区")
	}
	cfg.Timezone = "Not/AZone"
	if cfg.Location() != time.Local {
		t.Error("非法时区应退回本地时区")
	}
	cfg.Timezone = "UTC"
	if cfg.Location() != time.UTC {
		t.Error("UTC 应被正确解析")
	}
}
