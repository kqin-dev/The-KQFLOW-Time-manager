package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeConfig 直接写一份原始 config.json，用于伪造各种版本与损坏情况。
func writeConfig(t *testing.T, dir, content string) *Paths {
	t.Helper()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Paths{Root: dir, ConfigFile: path}
}

// TestLoadRejectsFutureConfig 验证来自更新版本的配置会被拒绝，
// 而不是被静默退回默认值再整份写回。
//
// 这是本批「数据版本保护」里最容易漏、后果最直接的一条：配置文件与程序
// 版本不匹配时，旧程序会把新版本的设置按当前结构重写一遍，用户的设置就没了。
func TestLoadRejectsFutureConfig(t *testing.T) {
	dir := t.TempDir()
	future := ConfigSchemaVersion + 1
	p := writeConfig(t, dir, `{"schema_version":`+strconv.Itoa(future)+
		`,"day_cutoff":"04:00","nickname":"未来用户","some_new_field":123}`)

	cfg, err := Load(p)
	if err == nil {
		t.Fatalf("未来版本的配置必须被拒绝，实际读到了 %+v", cfg)
	}
	if cfg != nil {
		t.Errorf("拒绝时不应返回配置，实际 %+v", cfg)
	}
	if !IsIncompatibleConfig(err) {
		t.Fatalf("错误类型应为 IncompatibleConfigError，实际 %T", err)
	}
	var target *IncompatibleConfigError
	if !errors.As(err, &target) {
		t.Fatal("errors.As 应能取出 IncompatibleConfigError")
	}
	if target.Current != ConfigSchemaVersion || target.Found != future {
		t.Errorf("版本号应为本程序 %d / 文件 %d，实际 %d / %d",
			ConfigSchemaVersion, future, target.Current, target.Found)
	}
	msg := err.Error()
	for _, want := range []string{"schema_version", "升级", "移出数据目录", p.ConfigFile} {
		if !strings.Contains(msg, want) {
			t.Errorf("错误文案应包含 %q，实际:\n%s", want, msg)
		}
	}

	// 关键：文件必须原样留着，一个字节都不能被改写。
	raw, readErr := os.ReadFile(p.ConfigFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(raw), "some_new_field") {
		t.Error("拒绝启动时不应改动配置文件内容")
	}
}

// TestLoadAcceptsCurrentAndOlderConfig 验证当前版本、更早版本、缺版本字段的配置都放行。
func TestLoadAcceptsCurrentAndOlderConfig(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"当前版本", `{"schema_version":` + strconv.Itoa(ConfigSchemaVersion) + `,"nickname":"甲"}`},
		{"更早版本", `{"schema_version":1,"nickname":"乙"}`},
		{"缺版本字段", `{"nickname":"丙"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			p := writeConfig(t, dir, c.content)
			if _, err := Load(p); err != nil {
				t.Errorf("应放行，实际报错: %v", err)
			}
		})
	}
}

// TestLoadStillToleratesBrokenConfig 验证「损坏」与「来自更新版本」被区别对待：
// 损坏仍然退回默认值不阻断启动（原有行为，不要被这次改动带坏）。
func TestLoadStillToleratesBrokenConfig(t *testing.T) {
	dir := t.TempDir()
	p := writeConfig(t, dir, `{ 这不是 JSON`)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("损坏的配置不应阻断启动，实际报错: %v", err)
	}
	if cfg == nil {
		t.Fatal("损坏的配置应退回默认值")
	}
	if cfg.DayCutoff == "" {
		t.Error("退回的默认配置应可用")
	}
}

// TestLoadMissingConfigWritesDefault 验证首次启动会写出默认配置（原有行为）。
func TestLoadMissingConfigWritesDefault(t *testing.T) {
	dir := t.TempDir()
	p := &Paths{Root: dir, ConfigFile: filepath.Join(dir, FileName)}

	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Fatal("应返回默认配置")
	}
	if _, err := os.Stat(p.ConfigFile); err != nil {
		t.Errorf("首次启动应写出一份默认配置: %v", err)
	}
}

// TestSaveNeverDowngradesConfigVersion 验证保存不会把版本号写低。
//
// 万一将来某条路径在未把关的情况下拿到了高版本配置，也不能由 Save 把它降级。
func TestSaveNeverDowngradesConfigVersion(t *testing.T) {
	dir := t.TempDir()
	p := &Paths{Root: dir, ConfigFile: filepath.Join(dir, FileName)}

	cfg := Default()
	cfg.SchemaVersion = 0
	if err := Save(p, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.SchemaVersion != ConfigSchemaVersion {
		t.Errorf("保存后版本号应为 %d，实际 %d", ConfigSchemaVersion, cfg.SchemaVersion)
	}

	higher := Default()
	higher.SchemaVersion = ConfigSchemaVersion + 5
	if err := Save(p, higher); err != nil {
		t.Fatal(err)
	}
	if higher.SchemaVersion != ConfigSchemaVersion+5 {
		t.Errorf("Save 不应把版本号写低，实际 %d", higher.SchemaVersion)
	}
}
