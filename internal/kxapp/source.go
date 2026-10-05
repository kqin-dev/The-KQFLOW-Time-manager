// Package kxapp 是 KQFLOW 与 KXFLOW 引擎之间的适配层。
//
// 它的职责可以用一句话说清：**把 KQFLOW 的业务数据接到引擎的插件体系上**。
// 按设计文档 §5，KQFLOW 在这里被拆成"内核 + 若干整合包"，
// 而 `internal/ui` 里那套原型界面一行不改——两者并行，
// 由宿主配置里的开关切换（M5 才切默认）。
//
// 依赖方向值得强调：kxapp **认识** model/store/config/clock，
// 而引擎（kxflow/*）**完全不认识**它们。这条边界由
// kxflow/internal/enginetest 的架构测试守着——引擎可以被单独拆出去。
package kxapp

import (
	"time"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/config"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/store"
)

// Source 是包们读写业务数据的唯一入口。
//
// 为什么要有这一层而不是让各包直接用 *store.Store：
//  1. 包只需要"读当天数据 / 读目标 / 写回"，不该认识按日分片、原子写、
//     备份与恢复这些存储细节；
//  2. 测试可以给一个内存实现，不必落盘（引擎与宿主都能离线验证）；
//  3. 将来换存储（比如加一层缓存）只改这里，不动任何包。
type Source interface {
	// Now 返回当前时间（走注入的时钟，保证测试确定性）。
	Now() time.Time
	// Config 返回配置。返回只读副本语义：**调用方不得修改**。
	Config() *config.Config
	// Day 返回当前逻辑日的日数据（可能为 nil，例如首次启动）。
	Day() *model.DayData
	// Goals 返回活跃的长期目标（goals.json 里的）。
	Goals() []model.Goal
	// Save 把当前日数据写回存储。
	Save() error
	// SaveGoals 把活跃目标写回存储。
	SaveGoals() error
	// SaveConfig 把配置写回存储。
	//
	// 设置页改的是"偏好"，与日数据是**两个文件**：混在一起会让
	// "改个昵称"也触发一次日数据备份（v2.1.0 的备份目录里就有大量
	// 这种无意义副本）。因此单独一个入口。
	SaveConfig() error
	// Reload 重新按当前时间计算逻辑日并载入数据。
	//
	// 跨日界线时需要它：界面停留过夜后，"今天"要变成新的一天。
	Reload() error
}

// storeSource 是基于真实 store 的实现。
type storeSource struct {
	st    *store.Store
	cfg   *config.Config
	paths *config.Paths
	clock func() time.Time

	day   string
	data  *model.DayData
	goals []model.Goal
	// goalsDirty 为真表示 goals 已被包改过、需要写回。
	//
	// 为什么要这个标记：`Goals()` 返回的是切片副本，包改不了它；
	// 而"完成目标"这个动作**必须**同时改 goals.json 与当日归档
	// （见需求 10 的双向搬移）。因此由包调用 SetGoals 把新列表交回来，
	// 再由 SaveGoals 落盘。标记脏值是为了避免把没改过的列表重复写盘。
	goalsDirty bool
}

// NewStoreSource 用真实存储构造数据源，并立刻载入当天数据。
func NewStoreSource(st *store.Store, paths *config.Paths, cfg *config.Config, now func() time.Time) (Source, error) {
	s := &storeSource{st: st, cfg: cfg, paths: paths, clock: now}
	if err := s.Reload(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *storeSource) Now() time.Time         { return s.clock() }
func (s *storeSource) Config() *config.Config { return s.cfg }
func (s *storeSource) Day() *model.DayData    { return s.data }
func (s *storeSource) Goals() []model.Goal    { return s.goals }

// SetGoals 替换活跃目标列表（由包在搬移目标后调用）。
func (s *storeSource) SetGoals(goals []model.Goal) {
	s.goals = goals
	s.goalsDirty = true
}

// DayName 返回当前逻辑日（不在 Source 接口里，但它对诊断很有用）。
func (s *storeSource) DayName() string { return s.day }

// Reload 依据当前时间重算逻辑日并载入。
func (s *storeSource) Reload() error {
	now := s.clock()
	s.day = logicalDay(now, s.cfg)
	data, err := s.st.EnsureDay(s.day, now)
	if err != nil {
		return err
	}
	goals, err := s.st.Goals()
	if err != nil {
		return err
	}
	s.data = data
	s.goals = goals
	s.goalsDirty = false
	return nil
}

func (s *storeSource) Save() error {
	if s.data == nil {
		return nil
	}
	return s.st.SaveDay(s.data)
}

// SaveGoals 把活跃目标写回存储。
//
// 没改过就跳过：目标列表通常是只读的，"每次保存日数据都顺手写一遍 goals.json"
// 会让备份目录里堆满无意义的副本。
func (s *storeSource) SaveGoals() error {
	if !s.goalsDirty {
		return nil
	}
	if err := s.st.SaveGoals(s.goals); err != nil {
		return err
	}
	s.goalsDirty = false
	return nil
}

// SaveConfig 把配置写回存储。
//
// 顺带重算一次逻辑日：日界线本身是个可改的设置，改完"今天是哪一天"
// 可能立刻变了（例如从 04:00 改成 23:00）。不重算的话界面会停在
// 旧的逻辑日上，而用户刚改的正是决定它的那个值。
func (s *storeSource) SaveConfig() error {
	if s.paths == nil {
		return nil
	}
	if err := config.Save(s.paths, s.cfg); err != nil {
		return err
	}
	return s.Reload()
}
