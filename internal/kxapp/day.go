package kxapp

import (
	"time"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/config"
)

// logicalDay 计算某个时刻属于哪个逻辑日。
//
// 这里**刻意复用 internal/clock**，而不是在适配层重写一遍：
// 日界线（"今天从几点算起"）是这个项目里最容易出错的逻辑之一
// （历史上出现过"日界线永远解析失败"与"18:30 被算成 22:30"两次事故），
// 只能有一个实现。适配层不得为了"看起来自洽"而复制它。
func logicalDay(now time.Time, cfg *config.Config) string {
	return clock.LogicalDay(now, cfg.Cutoff())
}
