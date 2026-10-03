// Package store 是 KQFLOW 的本地数据层。
//
// 布局（见需求 4）：
//
//	<数据根目录>/
//	  config.json        配置
//	  goals.json         与日期无关的 GOAL
//	  days/
//	    2026-10.json     按日切分的数据库文件，一天一个文件
//
// 所有写入都走“临时文件 + 原子改名”，并在写入前备份上一版，
// 因此即使终端被强制关闭，也不会留下半个 JSON（见需求 21）。
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/clock"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/config"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// Store 提供全部数据读写能力。
type Store struct {
	root string
	// BackupKeep 是每个文件保留的历史版本数。
	backupKeep int
}

// Open 打开（或初始化）数据层。
func Open(root string) (*Store, error) {
	s := &Store{root: root, backupKeep: 5}
	for _, dir := range []string{s.root, s.daysDir(), s.backupDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建目录 %s 失败: %w", dir, err)
		}
	}
	return s, nil
}

// Root 返回数据根目录。
func (s *Store) Root() string { return s.root }

func (s *Store) daysDir() string   { return filepath.Join(s.root, "days") }
func (s *Store) backupDir() string { return filepath.Join(s.root, "backup") }
func (s *Store) goalsFile() string { return filepath.Join(s.root, "goals.json") }

func (s *Store) monthFile(day string) string {
	month := day
	if len(day) >= 7 {
		month = day[:7]
	}
	return filepath.Join(s.daysDir(), month+".json")
}

// monthFile 的内容是一个按日索引的容器。
type monthFile struct {
	SchemaVersion int                       `json:"schema_version"`
	Month         string                    `json:"month"`
	Days          map[string]*model.DayData `json:"days"`
}

// monthBackupPrefix 是月份文件备份使用的统一前缀。
//
// 备份名与月份文件本身的文件名保持一致，恢复逻辑才能按文件名找回对应的备份。
func monthBackupPrefix(day string) string {
	month := day
	if len(day) >= 7 {
		month = day[:7]
	}
	return month
}

// ---------- GOAL ----------

// Goals 读取全部目标。
func (s *Store) Goals() ([]model.Goal, error) {
	raw, err := os.ReadFile(s.goalsFile())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取 %s 失败: %w", s.goalsFile(), err)
	}
	var book model.GoalBook
	if err := json.Unmarshal(raw, &book); err != nil {
		// 数据损坏时退回备份，尽量不让用户丢数据。
		if recovered, ok := s.recoverGoals(); ok {
			return recovered, nil
		}
		return nil, fmt.Errorf("解析 goals.json 失败: %w", err)
	}
	return book.Goals, nil
}

func (s *Store) recoverGoals() ([]model.Goal, bool) {
	backups, _ := filepath.Glob(filepath.Join(s.backupDir(), "goals-*.json"))
	sort.Sort(sort.Reverse(sort.StringSlice(backups)))
	for _, path := range backups {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var book model.GoalBook
		if err := json.Unmarshal(raw, &book); err == nil {
			return book.Goals, true
		}
	}
	return nil, false
}

// SaveGoals 原子地写回全部目标。
func (s *Store) SaveGoals(goals []model.Goal) error {
	if goals == nil {
		goals = []model.Goal{}
	}
	book := model.GoalBook{SchemaVersion: model.SchemaVersion, Goals: goals}
	return s.writeJSON(s.goalsFile(), book, "goals")
}

// ---------- 每日数据 ----------

// Day 读取某一逻辑日的数据；文件不存在时返回 nil。
func (s *Store) Day(day string) (*model.DayData, error) {
	m, err := s.loadMonth(day)
	if err != nil {
		return nil, err
	}
	if data, ok := m.Days[day]; ok && data != nil {
		normalizeDay(data, day)
		return data, nil
	}
	return nil, nil
}

// EnsureDay 读取某一逻辑日的数据，不存在时新建并落盘。
func (s *Store) EnsureDay(day string, now time.Time) (*model.DayData, error) {
	data, err := s.Day(day)
	if err != nil {
		return nil, err
	}
	if data != nil {
		return data, nil
	}
	data = model.NewDayData(day, now)
	if err := s.SaveDay(data); err != nil {
		return nil, err
	}
	return data, nil
}

// SaveDay 原子地保存某一逻辑日的数据。
func (s *Store) SaveDay(data *model.DayData) error {
	if data == nil {
		return fmt.Errorf("保存的日数据为空")
	}
	if data.Day == "" {
		return fmt.Errorf("日数据缺少日期")
	}
	data.SchemaVersion = model.SchemaVersion
	data.UpdatedAt = time.Now()
	m, err := s.loadMonth(data.Day)
	if err != nil {
		return err
	}
	if m.Days == nil {
		m.Days = map[string]*model.DayData{}
	}
	m.Days[data.Day] = data
	return s.writeJSON(s.monthFile(data.Day), m, monthBackupPrefix(data.Day))
}

// Days 返回所有已存在的逻辑日，按时间升序。
func (s *Store) Days() ([]string, error) {
	entries, err := os.ReadDir(s.daysDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	seen := map[string]bool{}
	var days []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		m, err := s.loadMonthFile(filepath.Join(s.daysDir(), e.Name()))
		if err != nil {
			continue
		}
		for day := range m.Days {
			if !seen[day] {
				seen[day] = true
				days = append(days, day)
			}
		}
	}
	sort.Strings(days)
	return days, nil
}

// RecentDays 返回最近 n 个存在数据的逻辑日，按时间升序。
func (s *Store) RecentDays(n int) ([]string, error) {
	days, err := s.Days()
	if err != nil {
		return nil, err
	}
	if n > 0 && len(days) > n {
		days = days[len(days)-n:]
	}
	return days, nil
}

// ---------- 继承 ----------

// CarryResult 描述一次“从昨天继承”的结果。
type CarryResult struct {
	// FixedAdded 是从昨日固定清单补入的条目数。
	FixedAdded int
	// FloatingAdded 是从昨日未完成的临时清单补入的条目数。
	FloatingAdded int
	// From 是来源日期。
	From string
}

// CarryFixed 把昨日的固定 TODO 复制到今日固定清单（见需求 14）。
//
// 固定 TODO 与完成状态无关：它是用户长期坚持的事项，每天都应该出现。
func (s *Store) CarryFixed(prevDay, day string, now time.Time) (int, error) {
	prev, err := s.Day(prevDay)
	if err != nil || prev == nil {
		return 0, err
	}
	today, err := s.EnsureDay(day, now)
	if err != nil {
		return 0, err
	}
	if today.CarryAsked {
		return 0, nil
	}
	existing := map[string]bool{}
	for _, t := range today.Fixed {
		existing[strings.ToLower(strings.TrimSpace(t.Title))] = true
	}
	added := 0
	for _, src := range prev.Fixed {
		key := strings.ToLower(strings.TrimSpace(src.Title))
		if key == "" || existing[key] {
			continue
		}
		item := model.NewTodo(src.Title, model.KindFixed, day, now)
		item.CarriedFrom = prevDay
		item.GoalRef, item.GoalTag = src.GoalRef, src.GoalTag
		// 固定事项的分解任务也一起带过来，但重置为未完成。
		for _, task := range src.Tasks {
			item.Tasks = append(item.Tasks, model.NewTask(task.Title))
		}
		today.Fixed = append(today.Fixed, item)
		existing[key] = true
		added++
	}
	today.CarryAsked = true
	if err := s.SaveDay(today); err != nil {
		return added, err
	}
	return added, nil
}

// CarryFloating 把昨日未完成的临时 TODO 继承到今日（见需求 14）。
func (s *Store) CarryFloating(prevDay, day string, now time.Time) (int, error) {
	prev, err := s.Day(prevDay)
	if err != nil || prev == nil {
		return 0, err
	}
	today, err := s.EnsureDay(day, now)
	if err != nil {
		return 0, err
	}
	existing := map[string]bool{}
	for _, t := range today.Floating {
		existing[strings.ToLower(strings.TrimSpace(t.Title))] = true
	}
	added := 0
	for _, src := range prev.Floating {
		if src.Done {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(src.Title))
		if key == "" || existing[key] {
			continue
		}
		item := model.NewTodo(src.Title, model.KindFloating, day, now)
		item.CarriedFrom = prevDay
		item.GoalRef, item.GoalTag = src.GoalRef, src.GoalTag
		for _, task := range src.Tasks {
			if task.Done() {
				continue
			}
			item.Tasks = append(item.Tasks, model.NewTask(task.Title))
		}
		today.Floating = append(today.Floating, item)
		existing[key] = true
		added++
	}
	today.CarryAsked = true
	if err := s.SaveDay(today); err != nil {
		return added, err
	}
	return added, nil
}

// ---------- 内部 ----------

func (s *Store) loadMonth(day string) (*monthFile, error) {
	return s.loadMonthFile(s.monthFile(day))
}

func (s *Store) loadMonthFile(path string) (*monthFile, error) {
	m := &monthFile{SchemaVersion: model.SchemaVersion, Days: map[string]*model.DayData{}}
	m.Month = strings.TrimSuffix(filepath.Base(path), ".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	if err := json.Unmarshal(raw, m); err != nil {
		if recovered, ok := s.recoverMonth(path); ok {
			return recovered, nil
		}
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	if m.Days == nil {
		m.Days = map[string]*model.DayData{}
	}
	return m, nil
}

func (s *Store) recoverMonth(path string) (*monthFile, bool) {
	base := strings.TrimSuffix(filepath.Base(path), ".json")
	backups, _ := filepath.Glob(filepath.Join(s.backupDir(), base+"-*.json"))
	sort.Sort(sort.Reverse(sort.StringSlice(backups)))
	for _, bp := range backups {
		raw, err := os.ReadFile(bp)
		if err != nil {
			continue
		}
		m := &monthFile{SchemaVersion: model.SchemaVersion, Days: map[string]*model.DayData{}}
		if err := json.Unmarshal(raw, m); err == nil {
			if m.Days == nil {
				m.Days = map[string]*model.DayData{}
			}
			return m, true
		}
	}
	return nil, false
}

// writeJSON 先把当前版本备份，再原子写入新内容。
func (s *Store) writeJSON(path string, v any, backupPrefix string) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}
	raw = append(raw, '\n')
	if err := s.backup(path, backupPrefix); err != nil {
		// 备份失败不阻断主写入，但要尽量保证主数据落盘。
		_ = err
	}
	if err := config.AtomicWrite(path, raw); err != nil {
		return err
	}
	s.pruneBackups(backupPrefix)
	return nil
}

// backup 把现有文件复制一份到 backup 目录，带时间戳。
func (s *Store) backup(path, prefix string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	stamp := time.Now().Format("20060102-150405.000000000")
	target := filepath.Join(s.backupDir(), fmt.Sprintf("%s-%s.json", prefix, stamp))
	return os.WriteFile(target, raw, 0o644)
}

func (s *Store) pruneBackups(prefix string) {
	files, _ := filepath.Glob(filepath.Join(s.backupDir(), prefix+"-*.json"))
	if len(files) <= s.backupKeep {
		return
	}
	sort.Strings(files)
	for _, f := range files[:len(files)-s.backupKeep] {
		os.Remove(f)
	}
}

// normalizeDay 修补从磁盘读入的数据，保证字段自洽。
func normalizeDay(data *model.DayData, day string) {
	if data.Day == "" {
		data.Day = day
	}
	if data.Activity == nil {
		data.Activity = map[string]*model.Activity{}
	}
	if data.Archive.Day == "" {
		data.Archive.Day = day
	}
	for _, t := range data.Fixed {
		if t.Kind == "" {
			t.Kind = model.KindFixed
		}
	}
	for _, t := range data.Floating {
		if t.Kind == "" {
			t.Kind = model.KindFloating
		}
	}
}

// ParseDay 校验并返回逻辑日字符串。
func ParseDay(day string) (string, error) {
	if _, err := time.ParseInLocation(clock.DateFormat, day, time.Local); err != nil {
		return "", fmt.Errorf("日期 %q 格式不正确，应为 YYYY-MM-DD", day)
	}
	return day, nil
}
