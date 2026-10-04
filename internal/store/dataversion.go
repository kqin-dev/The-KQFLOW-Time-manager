package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// 数据版本保护（见 bug.md 注意 1）。
//
// 数据结构随版本演进时，旧程序读到新版本的数据可能把不认识的字段直接抹掉
// （本项目是整份 JSON 反序列化后再整份写回，未知字段会被丢弃）。因此这里采取
// **保守策略**：只要发现数据来自更新的版本，就拒绝启动，请用户先升级程序，
// 或把不兼容的数据文件移走保存。
//
// 只读取、绝不写入——发现未来版本后不能留下任何痕迹，否则用户想回退都没有
// 干净的数据可用。

// IncompatibleDataError 报告数据文件来自更新的版本，本程序不能安全处理。
type IncompatibleDataError struct {
	// Current 是本程序支持的数据版本。
	Current int
	// Files 是不兼容的文件路径，按路径排序，便于直接展示给用户。
	Files []string
	// Versions 与 Files 一一对应，是对应文件的 schema_version。
	Versions []int
	// Broken 是能定位到但读不出 schema_version 的文件及其原因。
	//
	// 它必须一起阻断启动：认不出内容就不能保证它不来自更新的版本，
	// 而「读进来再整份写回」正会把不认识的字段抹掉。
	Broken map[string]string
}

func (e *IncompatibleDataError) Error() string {
	var b strings.Builder
	if len(e.Files) > 0 {
		b.WriteString(fmt.Sprintf("数据来自更新的版本（本程序支持 schema_version ≤ %d）:\n", e.Current))
		for i, f := range e.Files {
			fmt.Fprintf(&b, "  %s（schema_version=%d）\n", f, e.Versions[i])
		}
	}
	if len(e.Broken) > 0 {
		b.WriteString("以下数据文件无法识别，不能确认它们不来自更新的版本：\n")
		paths := make([]string, 0, len(e.Broken))
		for p := range e.Broken {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			fmt.Fprintf(&b, "  %s（%s）\n", p, e.Broken[p])
		}
	}
	b.WriteString("请升级到最新版本的程序后再启动；\n")
	b.WriteString("若确实要用旧程序打开，请先把上面这些文件移出数据目录（另存备份），再重新启动。")
	return b.String()
}

// MaxVersion 返回发现的最大 schema_version。
func (e *IncompatibleDataError) MaxVersion() int {
	max := 0
	for _, v := range e.Versions {
		if v > max {
			max = v
		}
	}
	return max
}

// schemaPeek 只取文件里的版本与结构性字段，避免把不认识的数据整个读进来。
type schemaPeek struct {
	SchemaVersion int `json:"schema_version"`
}

// CheckDataVersion 检查数据目录里已知的数据文件是否来自当前或更早的版本。
//
// 覆盖 goals.json 与 days/*.json；config.json 由 config.Load 自行把关
// （它住在数据根目录，但由 config 包负责读写）。
//
// 缺失的文件直接跳过（首次启动就是这样）；但**存在却读不出内容**的文件要阻断
// 启动。理由：认不出内容就无法保证它不来自更新的版本，而本项目是整份 JSON
// 读进来再整份写回——一个读不懂的文件被写回，等于把不认识的字段抹掉。
// 实测踩到的例子：带 UTF-8 BOM 的文件（Windows PowerShell 的
// `Set-Content -Encoding utf8` 就会写出 BOM）解析失败，早期版本在这里选择
// “跳过”，结果照样启动并开始写数据。
func CheckDataVersion(root string) error {
	var (
		files    []string
		versions []int
		broken   = map[string]string{}
	)
	check := func(path string) {
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return
			}
			broken[path] = err.Error()
			return
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			// 空文件没有数据结构可言，交给各自的容错路径。
			return
		}
		var peek schemaPeek
		if err := json.Unmarshal(raw, &peek); err != nil {
			broken[path] = "无法解析为 JSON"
			return
		}
		if peek.SchemaVersion > model.SchemaVersion {
			files = append(files, path)
			versions = append(versions, peek.SchemaVersion)
		}
	}

	check(filepath.Join(root, "goals.json"))

	entries, err := os.ReadDir(filepath.Join(root, "days"))
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			check(filepath.Join(root, "days", e.Name()))
		}
	}

	if len(files) == 0 && len(broken) == 0 {
		return nil
	}
	// 按路径排序，输出稳定，便于用户按清单操作，也便于测试断言。
	type pair struct {
		path string
		ver  int
	}
	pairs := make([]pair, len(files))
	for i := range files {
		pairs[i] = pair{files[i], versions[i]}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].path < pairs[j].path })
	for i, p := range pairs {
		files[i], versions[i] = p.path, p.ver
	}
	return &IncompatibleDataError{Current: model.SchemaVersion, Files: files, Versions: versions, Broken: broken}
}

// IsIncompatibleData 报告错误是否属于「数据来自更新版本」。
func IsIncompatibleData(err error) bool {
	var target *IncompatibleDataError
	return errors.As(err, &target)
}
