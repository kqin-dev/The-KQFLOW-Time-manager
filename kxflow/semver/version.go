// Package semver 提供最小可用的语义化版本与版本范围。
//
// 为什么自己写而不引第三方：引擎的依赖越少越容易独立分发，而这套需求很小
// （比较、解析、以及 ">=0.1 <0.2" 这类由空格分隔的合取式范围）。
// 版本范围是插件体系的地基——`Manifest.EngineAPI` 就是靠它裁决
// "这个插件能不能装在这个引擎上"，因此它必须有测试、可预测。
//
// **0.x 阶段的约定**（design §4.5）：minor 即破坏性变更位。
// 0.1 → 0.2 是破坏性的，因此插件写 `>=0.1 <0.2` 才安全。
package semver

import (
	"fmt"
	"strconv"
	"strings"
)

// Version 是 major.minor.patch 三段版本号。
//
// 不处理 pre-release / build metadata：KXFLOW 的 EngineAPI 用它，
// 官方的 -dev.N 后缀属于宿主自己的程序版本，与引擎 API 无关。
type Version struct {
	Major, Minor, Patch int
}

// Version 构造一个版本。
func New(major, minor, patch int) Version {
	return Version{Major: major, Minor: minor, Patch: patch}
}

// Parse 解析 "0.1.0" 形式的版本号。
func Parse(s string) (Version, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return Version{}, fmt.Errorf("版本号 %q 格式不对，应为 major[.minor[.patch]]", s)
	}
	var nums [3]int
	for i, p := range parts {
		// 允许 "0.1.x" 这类通配写法在范围里出现，但单个版本号不接受。
		if p == "" {
			return Version{}, fmt.Errorf("版本号 %q 有空段", s)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Version{}, fmt.Errorf("版本号 %q 的段 %q 不是非负整数", s, p)
		}
		nums[i] = n
	}
	return Version{Major: nums[0], Minor: nums[1], Patch: nums[2]}, nil
}

// MustParse 在解析失败时 panic。
//
// 只允许用于**常量声明与测试夹具**：写死一个版本号却写错格式是开发者错误，
// 应当在启动/测试时立刻炸掉，而不是留到运行期变成一个"莫名其妙的版本不匹配"。
func MustParse(s string) Version {
	v, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return v
}

// String 返回 "major.minor.patch"。
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compare 返回 -1 / 0 / 1。
func (v Version) Compare(o Version) int {
	switch {
	case v.Major != o.Major:
		return sign(v.Major - o.Major)
	case v.Minor != o.Minor:
		return sign(v.Minor - o.Minor)
	case v.Patch != o.Patch:
		return sign(v.Patch - o.Patch)
	}
	return 0
}

// Less / Greater / Equal 是便于阅读的包装。
func (v Version) Less(o Version) bool    { return v.Compare(o) < 0 }
func (v Version) Greater(o Version) bool { return v.Compare(o) > 0 }
func (v Version) Equal(o Version) bool   { return v.Compare(o) == 0 }

// Compatible 报告在给定范围下 o 是否与 v 兼容。
//
// 语义是"按 0.x 规则的可替换性"：主版本相同即兼容；主版本为 0 时，
// minor 也必须相同（0.x 阶段 minor 即破坏性位）。
// 这是给"插件要不要跟随引擎升级"用的辅助判断，裁决仍以 Range 为准。
func (v Version) Compatible(o Version) bool {
	if v.Major != o.Major {
		return false
	}
	if v.Major == 0 {
		return v.Minor == o.Minor
	}
	return true
}

func sign(n int) int {
	if n < 0 {
		return -1
	}
	if n > 0 {
		return 1
	}
	return 0
}
