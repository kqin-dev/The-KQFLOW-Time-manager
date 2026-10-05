package canvas

import "github.com/mattn/go-runewidth"

// 宽度口径必须**确定且显式**，不能依赖环境自动判断。
//
// 这是一个真实的坑，而且有两个方向，两边都踩过：
//
//  1. **不设它**（go-runewidth 的默认行为）：库会按环境（LANG / 代码页 / 是否检测到
//     CJK 区域）自行决定。同一份代码在本机测出「中」宽 1、在用户终端上宽 2，
//     于是所有列位置判断一起偏——而测试仍然全绿。KQFLOW v2.1.0 那类
//     "只在特定终端下出现、开发环境复现不了"的排版问题，根因就是这个：
//     **同一个量存在两套口径**。
//  2. **设成 true**：go-runewidth 把一大批 Ambiguous 类字符也按 2 列算，
//     其中包含本项目赖以拼版面的框线（╭ ─ │ ╰）与几何图形（○ ◐ ◈ ▏ …）。
//     实测：`╭` 会变成 2 列，一行 9 个字符报出 15 的宽度 —— 边框全部错位。
//
// 因此这里显式钉成 false，并用 TestWidthContractIsDeterministic 固化：
//   - 中日韩文字与全角标点（中 好 ，。：（）「」）：2 列
//   - ASCII、框线、几何图形（a 0 ╭ ─ │ ○ ◐ ◈ ▏ …）：1 列
//
// 已知取舍：`…`（U+2026）属于 Ambiguous，在部分东亚环境下终端会渲染成 2 列。
// 本项目大量用它做截断省略号，按 1 列算是与现有版式一致的唯一选择。
// 改这条口径必须同时改版式与测试，不允许"顺手改一下"。
func init() {
	runewidth.DefaultCondition.EastAsianWidth = false
	runewidth.DefaultCondition.StrictEmojiNeutral = false
}

// EastAsianWidth 报告当前宽度口径下东亚"歧义宽度"字符是否按 2 列计算。
//
// 供宿主做自检：插件可以在启动时断言运行环境的宽度口径符合预期。
// 引擎**不**提供修改它的入口——口径一旦允许运行期改动，同一帧里不同组件
// 就会用不同尺子，正是上面第 1 条要避免的。
func EastAsianWidth() bool { return runewidth.DefaultCondition.EastAsianWidth }
