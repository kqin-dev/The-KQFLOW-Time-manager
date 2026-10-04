// 包 version 保存 KQFLOW 的版本号，是全项目唯一的权威来源。
//
// 版本号在发布时更新这里即可；界面、-version 输出都读同一个值，
// 不会出现“界面显示一个版本、命令行显示另一个”的情况。
package version

// Version 是当前版本号，随每次发布更新。
//
// 编译时可以用 -ldflags 覆盖，用于本地或 CI 打特定版本：
//
//	go build -ldflags "-X github.com/kqin-dev/The-KQFLOW-Time-manager/internal/version.Version=1.2.3"
//
// 没覆盖时就用这里写的值，保证任何构建产物都有正确的版本号。
var Version = "2.1.0"

// String 返回适合展示的版本号（带 v 前缀）。
func String() string { return "v" + Version }
