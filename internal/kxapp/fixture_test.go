package kxapp

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kqin-dev/kxflow/plugin"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/config"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// memSource 是内存版数据源。
//
// 它让宿主层可以**完全离线**测试：不建目录、不落盘、不依赖系统时间。
// 同时它是 Source 接口的第二个实现，这本身就证明了抽象是成立的
// （只有一个实现的接口通常只是把结构体拆成两半）。
type memSource struct {
	cfg   *config.Config
	now   time.Time
	data  *model.DayData
	goals []model.Goal
	// extraDays 非空时由 RecentDays 返回它（用来构造"多天历史"）。
	extraDays []*model.DayData

	saves       int
	goalsSaved  int
	configSaved int
	saveErr     error
}

func newMemSource(t *testing.T, now time.Time) *memSource {
	t.Helper()
	cfg := config.Default()
	// 日界线 04:00：与 v2.1.0 的默认一致，也让"20:00 属于哪个逻辑日"
	// 这件事依赖真实实现（而不是碰巧相同）。
	cfg.DayCutoff = "04:00"
	s := &memSource{cfg: cfg, now: now}
	s.data = model.NewDayData(logicalDay(now, cfg), now)
	return s
}

func (m *memSource) Now() time.Time         { return m.now }
func (m *memSource) Config() *config.Config { return m.cfg }
func (m *memSource) Day() *model.DayData    { return m.data }
func (m *memSource) Goals() []model.Goal    { return m.goals }

func (m *memSource) SetGoals(goals []model.Goal) { m.goals = goals }

func (m *memSource) Save() error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saves++
	return nil
}

func (m *memSource) SaveGoals() error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.goalsSaved++
	return nil
}

// SaveConfig 记录配置写盘次数。
//
// 内存实现也要支持它——这正是"Source 是接口"的价值：
// 宿主层可以完全离线地被测试，而接口一旦加了方法，
// 所有实现都会在编译期被逼着补上（不会漏）。
func (m *memSource) SaveConfig() error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.configSaved++
	return nil
}

func (m *memSource) Reload() error { return nil }

// PrevDay 返回内存里最近一个早于今天的日子。
func (m *memSource) PrevDay() string {
	if m.data == nil {
		return ""
	}
	prev := ""
	for _, d := range m.extraDays {
		if d != nil && d.Day < m.data.Day && d.Day > prev {
			prev = d.Day
		}
	}
	return prev
}

// Carry 模拟继承：把昨日条目复制到今天，规则与 store 保持一致
// （固定项全带、临时项只带未完成的、同名不重复）。
func (m *memSource) Carry(mode string) (int, error) {
	prev := m.PrevDay()
	if prev == "" || m.data == nil {
		return 0, errors.New("没有可继承的昨日数据")
	}
	var prevData *model.DayData
	for _, d := range m.extraDays {
		if d != nil && d.Day == prev {
			prevData = d
			break
		}
	}
	if prevData == nil {
		return 0, errors.New("昨日没有数据")
	}
	added := 0
	exists := func(list []*model.Todo) map[string]bool {
		out := map[string]bool{}
		for _, t := range list {
			out[strings.ToLower(strings.TrimSpace(t.Title))] = true
		}
		return out
	}
	if mode == "fixed" || mode == "both" {
		have := exists(m.data.Fixed)
		for _, src := range prevData.Fixed {
			key := strings.ToLower(strings.TrimSpace(src.Title))
			if key == "" || have[key] {
				continue
			}
			item := model.NewTodo(src.Title, model.KindFixed, m.data.Day, m.now)
			item.CarriedFrom = prev
			for _, task := range src.Tasks {
				item.Tasks = append(item.Tasks, model.NewTask(task.Title))
			}
			m.data.Fixed = append(m.data.Fixed, item)
			have[key] = true
			added++
		}
	}
	if mode == "floating" || mode == "both" {
		have := exists(m.data.Floating)
		for _, src := range prevData.Floating {
			if src.Done {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(src.Title))
			if key == "" || have[key] {
				continue
			}
			item := model.NewTodo(src.Title, model.KindFloating, m.data.Day, m.now)
			item.CarriedFrom = prev
			for _, task := range src.Tasks {
				if task.Done() {
					continue
				}
				item.Tasks = append(item.Tasks, model.NewTask(task.Title))
			}
			m.data.Floating = append(m.data.Floating, item)
			have[key] = true
			added++
		}
	}
	m.data.CarryAsked = true
	if err := m.Save(); err != nil {
		return added, err
	}
	return added, nil
}

// RecentDays 返回内存里的日数据，供历史页测试使用。
//
// 多天逻辑由 collectHistory 负责，这里只要能提供"若干天"就足以验证聚合口径；
// 真机上的多天数据走 storeSource（转发 store.RecentDays）。
func (m *memSource) RecentDays(limit int) ([]*model.DayData, error) {
	if m.extraDays != nil {
		out := m.extraDays
		if limit > 0 && len(out) > limit {
			out = out[len(out)-limit:]
		}
		return out, nil
	}
	if m.data == nil || limit <= 0 {
		return nil, nil
	}
	return []*model.DayData{m.data}, nil
}

// addTodo 往当天的列表里加一条待办。
func (m *memSource) addTodo(title string, kind model.Kind) *model.Todo {
	t := model.NewTodo(title, kind, m.data.Day, m.now)
	if kind == model.KindFixed {
		m.data.Fixed = append(m.data.Fixed, t)
	} else {
		m.data.Floating = append(m.data.Floating, t)
	}
	return t
}

// addGoal 加一个活跃目标。
func (m *memSource) addGoal(title string) *model.Goal {
	g := model.NewGoal(title, m.now)
	m.goals = append(m.goals, *g)
	return &m.goals[len(m.goals)-1]
}

// testNow 是一个固定的测试时刻（避开日界线边界）。
func testNow() time.Time {
	return time.Date(2026, 10, 3, 20, 0, 0, 0, time.Local)
}

// buildEngine 装载全部包并返回引擎模型。
func buildEngine(t *testing.T, src Source) (*plugin.LoadReport, *Services) {
	t.Helper()
	l := NewLoader(src, src.Config())
	_, services, rep := l.Build()
	return rep, services
}
