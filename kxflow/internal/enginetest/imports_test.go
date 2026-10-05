// Package enginetest 提供 KXFLOW 引擎的架构约束测试。
//
// 它不是工具库，而是**把架构规则变成会失败的测试**：设计文档 §2.2 要求
// 引擎绝不依赖宿主（KQFLOW），否则"将来把引擎拆出去独立发布"就只是一句话。
// 用 Go 的 import 图来自动校验，比写在文档里靠人记着可靠得多。
package enginetest

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// modulePath 是本引擎的模块路径。
const modulePath = "github.com/kqin-dev/kxflow"

// forbiddenPrefixes 列出引擎**不得**依赖的模块前缀。
//
// 每加一个宿主，就把它的路径加到这里——这是这条边界的唯一维护点。
var forbiddenPrefixes = []string{
	"github.com/kqin-dev/The-KQFLOW-Time-manager",
}

// TestEngineDoesNotImportHost 扫描本模块全部 Go 文件的 import，禁止反向依赖宿主。
//
// 实现上刻意不调用 `go list`（那会依赖工具链与网络），直接解析源码：
// 约束就是要"在任何环境都能跑"，包括离线。
func TestEngineDoesNotImportHost(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()
	scanned := 0

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// 跳过版本控制目录与本地缓存（本仓库把 GOCACHE/GOMODCACHE 放在工程内）。
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".gocache", ".gomodcache", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		scanned++
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("解析 %s 失败：%v", path, perr)
		}
		for _, imp := range f.Imports {
			p, uerr := strconv.Unquote(imp.Path.Value)
			if uerr != nil {
				continue
			}
			for _, bad := range forbiddenPrefixes {
				if p == bad || strings.HasPrefix(p, bad+"/") {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s 依赖了宿主模块 %q —— 引擎必须保持可独立拆分（design §2.2）",
						rel, p)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历失败：%v", err)
	}
	if scanned == 0 {
		t.Fatal("没有扫描到任何 Go 文件：测试本身失效了（路径或跳过规则写错）")
	}
}

// TestEngineModulePathIsStable 验证模块路径没被改动。
//
// 模块路径是插件 EngineAPI 兼容性声明的一部分（插件的 go.mod 会 require 它），
// 改一次就是一次破坏性变更，必须有意识地改这条测试。
func TestEngineModulePathIsStable(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(moduleRoot(t), "go.mod"))
	if err != nil {
		t.Fatalf("读取 go.mod 失败：%v", err)
	}
	if !strings.Contains(string(data), "module "+modulePath) {
		t.Fatalf("go.mod 的模块路径应保持 %q", modulePath)
	}
}

// TestEngineHasNoReplaceToHost 验证引擎的 go.mod 里没有指向宿主的 replace。
//
// 宿主（KQFLOW）可以用 replace 指向引擎以便联合开发；**反向绝对不行**，
// 那会让引擎在自己的模块里就绑死宿主。
func TestEngineHasNoReplaceToHost(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(moduleRoot(t), "go.mod"))
	if err != nil {
		t.Fatalf("读取 go.mod 失败：%v", err)
	}
	if strings.Contains(string(data), "replace") {
		for _, bad := range forbiddenPrefixes {
			if strings.Contains(string(data), bad) {
				t.Fatalf("引擎的 go.mod 不得 replace 宿主模块 %q", bad)
			}
		}
	}
}

// moduleRoot 返回 kxflow 模块根目录。
//
// 本包的测试文件位于 kxflow/internal/enginetest/，因此根目录是上两级。
func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("取工作目录失败：%v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}
