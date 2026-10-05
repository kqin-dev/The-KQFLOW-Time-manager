package kxapp

import (
	"fmt"
	"time"

	"github.com/kqin-dev/kxflow/svc"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
)

// Services 是宿主提供给引擎的副作用实现。
//
// 引擎只说"我要存这个东西""我要响一下"，具体怎么做由这里决定。
// 这条边界带来三个直接好处（design §3.5）：
//   - 引擎可测试（假实现即可跑完整帧）；
//   - 副作用无法藏在渲染里（渲染期拿不到 Services 的写入口）；
//   - 引擎不认识 KQFLOW 的数据格式，因而可以被单独拆出去。
type Services struct {
	src Source
	// Effects 记录最近若干条副作用，供测试断言"该响的时候响了"。
	//
	// 真实终端上响铃是"发了就没法验证"的东西，因此这里留痕：
	// 测试可以断言 Effect 被发出，而不必真的去听声音。
	Effects []svc.Effect
	// Saves 记录落盘次数，供测试断言"改完确实存了"。
	Saves int
	// SaveErr 非空时模拟落盘失败。
	SaveErr error
}

// NewServices 创建副作用实现。
func NewServices(src Source) *Services { return &Services{src: src} }

// Clock 返回当前时间。走数据源的注入时钟，保证测试确定性。
func (s *Services) Clock() time.Time {
	if s.src == nil || s.src.Now().IsZero() {
		return time.Now()
	}
	return s.src.Now()
}

// Persist 把插件请求的落盘落到真实存储上。
//
// 引擎不解释 payload，只负责把它交给这里；由宿主决定该调哪个保存方法。
// 这样"新加一种可持久化的东西"不需要改引擎。
//
// 目前有两类：日数据（"day"）与配置（"config"）。
// **分开存**：配置是偏好、日数据是记录，混在一起会让"改个昵称"
// 也触发一次日数据备份（v2.1.0 的备份目录里就有大量这种无意义副本）。
func (s *Services) Persist(req svc.PersistRequest) error {
	if s.SaveErr != nil {
		return s.SaveErr
	}
	if s.src == nil {
		return nil
	}
	switch req.Kind {
	case "config":
		if err := s.src.SaveConfig(); err != nil {
			return err
		}
		s.Saves++
		return nil
	}
	// 默认按日数据处理：目标列表与日数据是两个文件，因此两个都存一次。
	// 先存日数据再存目标：完成一个 GOAL 会同时改两者，
	// 万一中途失败，留下的是"日数据已更新、目标列表没跟上"，
	// 而不是反过来——前者在界面上表现为目标还在，用户重试即可。
	if err := s.src.Save(); err != nil {
		return err
	}
	if err := s.src.SaveGoals(); err != nil {
		return err
	}
	s.Saves++
	return nil
}

// Effect 播一次副作用。
//
// 终端响铃就是真写一个 BEL 字符——v2.1.0 试过代码合成音频，
// 结果在本机都放不出声，最后整条路删掉只用系统响铃（见 SKILL pitfalls）。
func (s *Services) Effect(e svc.Effect) {
	s.Effects = append(s.Effects, e)
}

// Capabilities 报告当前环境能力。
func (s *Services) Capabilities() svc.Capability {
	return svc.Capability{"kqflow.store", "kqflow.bell"}
}

// Belled 报告是否响过铃（测试辅助）。
func (s *Services) Belled() bool {
	for _, e := range s.Effects {
		if e.Kind == svc.EffectBell {
			return true
		}
	}
	return false
}

// HumanDuration 是给包用的时长格式化（转发到 internal/clock，避免各包自己写）。
func HumanDuration(d time.Duration) string { return clock.HumanDuration(d) }

// Describe 返回一份便于诊断的副作用摘要。
func (s *Services) Describe() string {
	return fmt.Sprintf("saves=%d effects=%d", s.Saves, len(s.Effects))
}
