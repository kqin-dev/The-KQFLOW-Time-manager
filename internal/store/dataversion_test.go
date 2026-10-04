package store_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/store"
)

// writeJSONFile 直接写一个 JSON 文件，用于伪造各种版本的数据。
func writeJSONFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// monthJSON 生成一份按月分片的日数据文件内容。
func monthJSON(t *testing.T, version int) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"schema_version": version,
		"month":          "2026-10",
		"days":           map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestCheckDataVersionAcceptsCurrentAndOlder 验证当前版本与更早版本的数据都放行。
func TestCheckDataVersionAcceptsCurrentAndOlder(t *testing.T) {
	cases := []struct {
		name  string
		month int
		goals int
	}{
		{"当前版本", model.SchemaVersion, model.SchemaVersion},
		{"更早版本", model.SchemaVersion - 1, model.SchemaVersion - 1},
		{"没有版本字段（老数据）", 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeJSONFile(t, filepath.Join(dir, "days", "2026-10.json"), monthJSON(t, c.month))
			writeJSONFile(t, filepath.Join(dir, "goals.json"),
				`{"schema_version":`+strconv.Itoa(c.goals)+`,"goals":[]}`)

			if err := store.CheckDataVersion(dir); err != nil {
				t.Errorf("应放行，实际报错: %v", err)
			}
		})
	}
}

// TestCheckDataVersionRejectsNewerData 验证未来版本的数据会被拒绝，
// 且错误里要能直接看出是哪个文件、什么版本。
func TestCheckDataVersionRejectsNewerData(t *testing.T) {
	dir := t.TempDir()
	future := model.SchemaVersion + 1
	writeJSONFile(t, filepath.Join(dir, "days", "2026-10.json"), monthJSON(t, future))
	writeJSONFile(t, filepath.Join(dir, "goals.json"),
		`{"schema_version":`+strconv.Itoa(model.SchemaVersion)+`,"goals":[]}`)

	err := store.CheckDataVersion(dir)
	if err == nil {
		t.Fatal("未来版本的数据必须被拒绝")
	}
	if !store.IsIncompatibleData(err) {
		t.Fatalf("错误类型应为 IncompatibleDataError，实际 %T", err)
	}

	var target *store.IncompatibleDataError
	if !errors.As(err, &target) {
		t.Fatal("errors.As 应能取出 IncompatibleDataError")
	}
	if target.Current != model.SchemaVersion {
		t.Errorf("本程序支持的版本应为 %d，实际 %d", model.SchemaVersion, target.Current)
	}
	if len(target.Files) != 1 {
		t.Fatalf("应有 1 个不兼容文件，实际 %d: %v", len(target.Files), target.Files)
	}
	if !strings.HasSuffix(target.Files[0], filepath.Join("days", "2026-10.json")) {
		t.Errorf("不兼容文件应是 days/2026-10.json，实际 %q", target.Files[0])
	}
	if target.MaxVersion() != future {
		t.Errorf("最大版本应为 %d，实际 %d", future, target.MaxVersion())
	}

	// 错误文案必须能让用户知道怎么办：升级程序，或把文件移走。
	msg := err.Error()
	for _, want := range []string{"schema_version", "升级", "移出数据目录"} {
		if !strings.Contains(msg, want) {
			t.Errorf("错误文案应包含 %q，实际:\n%s", want, msg)
		}
	}
	if !strings.Contains(msg, target.Files[0]) {
		t.Errorf("错误文案应列出不兼容文件路径，实际:\n%s", msg)
	}
}

// TestCheckDataVersionListsAllOffendingFiles 验证多个不兼容文件会一起列出，
// 且顺序稳定（用户要照单操作）。
func TestCheckDataVersionListsAllOffendingFiles(t *testing.T) {
	dir := t.TempDir()
	future := model.SchemaVersion + 3
	writeJSONFile(t, filepath.Join(dir, "goals.json"),
		`{"schema_version":`+strconv.Itoa(future)+`,"goals":[]}`)
	writeJSONFile(t, filepath.Join(dir, "days", "2026-11.json"), monthJSON(t, future))
	writeJSONFile(t, filepath.Join(dir, "days", "2026-10.json"), monthJSON(t, future))
	// 一个正常月份的干扰项，不该出现在清单里。
	writeJSONFile(t, filepath.Join(dir, "days", "2026-09.json"), monthJSON(t, model.SchemaVersion))

	err := store.CheckDataVersion(dir)
	var target *store.IncompatibleDataError
	if !errors.As(err, &target) {
		t.Fatalf("应报 IncompatibleDataError，实际 %v", err)
	}
	if len(target.Files) != 3 {
		t.Fatalf("应列出 3 个不兼容文件，实际 %d: %v", len(target.Files), target.Files)
	}
	for i := 1; i < len(target.Files); i++ {
		if target.Files[i-1] >= target.Files[i] {
			t.Errorf("文件清单应按路径排序，实际 %v", target.Files)
		}
	}
	for _, f := range target.Files {
		if strings.Contains(f, "2026-09") {
			t.Errorf("正常月份不该出现在清单里: %v", target.Files)
		}
	}
}

// TestCheckDataVersionIgnoresMissingAndIrrelevant 验证缺失文件与无关文件
// 不会阻断启动（首次启动就是「什么都还没有」）。
func TestCheckDataVersionIgnoresMissingAndIrrelevant(t *testing.T) {
	dir := t.TempDir()
	// 空目录。
	if err := store.CheckDataVersion(dir); err != nil {
		t.Errorf("空目录应放行，实际: %v", err)
	}
	// 空文件没有数据结构可言，交给各自的容错路径。
	writeJSONFile(t, filepath.Join(dir, "goals.json"), "")
	// 非 json 文件与子目录也不该被当成数据。
	writeJSONFile(t, filepath.Join(dir, "days", "notes.txt"), "schema_version: 999")
	if err := os.MkdirAll(filepath.Join(dir, "days", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckDataVersion(dir); err != nil {
		t.Errorf("缺失/空/无关文件应放行，实际: %v", err)
	}
}

// TestCheckDataVersionBlocksUnreadableData 验证存在却读不懂的数据文件会阻断启动。
//
// 这是实机验证时发现的漏洞：早期版本对解析失败的文件选择「跳过」，于是一个
// **带 UTF-8 BOM** 的未来版本文件（Windows PowerShell 的
// `Set-Content -Encoding utf8` 就会写出 BOM）被放过去，程序照常启动并开始
// 写数据——守卫形同虚设。认不出内容就不能保证它不是新版本的数据。
func TestCheckDataVersionBlocksUnreadableData(t *testing.T) {
	dir := t.TempDir()
	// 带 BOM 的合法 JSON：内容其实完全正常，只是前面多了 EF BB BF。
	bom := "\ufeff"
	writeJSONFile(t, filepath.Join(dir, "days", "2026-10.json"), bom+monthJSON(t, model.SchemaVersion))
	writeJSONFile(t, filepath.Join(dir, "goals.json"), "{ 这不是 JSON")

	err := store.CheckDataVersion(dir)
	if err == nil {
		t.Fatal("读不懂的数据文件必须阻断启动")
	}
	var target *store.IncompatibleDataError
	if !errors.As(err, &target) {
		t.Fatalf("应报 IncompatibleDataError，实际 %v", err)
	}
	if len(target.Broken) != 2 {
		t.Fatalf("应报告 2 个无法识别的文件，实际 %d: %v", len(target.Broken), target.Broken)
	}
	// 错误文案要把「读不懂」单独说清楚，而不是混进版本号清单里。
	msg := err.Error()
	if !strings.Contains(msg, "无法识别") {
		t.Errorf("错误文案应说明有文件无法识别，实际:\n%s", msg)
	}
	for _, f := range []string{"2026-10.json", "goals.json"} {
		if !strings.Contains(msg, f) {
			t.Errorf("错误文案应列出 %s，实际:\n%s", f, msg)
		}
	}
}

// TestCheckDataVersionDoesNotWrite 验证检查过程绝不改动数据目录。
//
// 这是保守策略的关键：发现未来版本后必须原样留着，用户想回退才有干净数据。
func TestCheckDataVersionDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	future := model.SchemaVersion + 1
	monthPath := filepath.Join(dir, "days", "2026-10.json")
	writeJSONFile(t, monthPath, monthJSON(t, future))

	before, err := os.ReadFile(monthPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CheckDataVersion(dir); err == nil {
		t.Fatal("应拒绝未来版本")
	}
	after, err := os.ReadFile(monthPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("检查过程改动了数据文件内容")
	}
	if _, err := os.Stat(filepath.Join(dir, "backup")); !os.IsNotExist(err) {
		t.Error("检查过程不应创建任何目录")
	}
}

// TestCheckDataVersionSeesReplacedFile 验证检查只看磁盘内容，
// 不依赖任何内存状态——main 会先检查再 Open，顺序不能反过来。
func TestCheckDataVersionSeesReplacedFile(t *testing.T) {
	dir := t.TempDir()
	// 先用正常版本写一份数据。
	writeJSONFile(t, filepath.Join(dir, "days", "2026-10.json"), monthJSON(t, model.SchemaVersion))
	if err := store.CheckDataVersion(dir); err != nil {
		t.Fatalf("正常版本应放行: %v", err)
	}
	// 再把它替换成未来版本。
	writeJSONFile(t, filepath.Join(dir, "days", "2026-10.json"), monthJSON(t, model.SchemaVersion+2))
	if err := store.CheckDataVersion(dir); err == nil {
		t.Error("替换为未来版本后应被拒绝")
	}
}
