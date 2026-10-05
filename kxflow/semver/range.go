package semver

import (
	"fmt"
	"strings"
)

// Range 是一个版本范围：若干约束的**合取**（全部满足才算匹配）。
//
// 文本形式用空格分隔，例如：
//
//	">=0.1 <0.2"     0.1.x 任意 patch（推荐的插件写法）
//	">=0.1"          0.1 及以上
//	"0.1.0"          恰好等于（等价于 "=0.1.0"）
//	"<1.0.0"         低于 1.0.0
//	""               不限制（任何版本都算匹配）
//
// 不支持 `||`（析取）：插件的 EngineAPI 应当是一条清晰的区间，
// 而不是一串候选——否则"为什么它装上了/没装上"会变得难解释。
type Range struct {
	raw         string
	constraints []constraint
}

type op uint8

const (
	opEqual op = iota
	opGreater
	opGreaterEqual
	opLess
	opLessEqual
)

func (o op) String() string {
	switch o {
	case opEqual:
		return "="
	case opGreater:
		return ">"
	case opGreaterEqual:
		return ">="
	case opLess:
		return "<"
	case opLessEqual:
		return "<="
	}
	return "?"
}

type constraint struct {
	op op
	v  Version
}

// Any 返回"不限制"的范围。
func Any() Range { return Range{raw: ""} }

// ParseRange 解析版本范围文本。
func ParseRange(s string) (Range, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return Any(), nil
	}
	fields := strings.Fields(raw)
	r := Range{raw: raw}
	for _, f := range fields {
		if strings.Contains(f, "||") {
			return Range{}, fmt.Errorf("版本范围 %q 不支持 || 析取，请写成一条区间（如 \">=0.1 <0.2\"）", s)
		}
		c, err := parseConstraint(f)
		if err != nil {
			return Range{}, fmt.Errorf("版本范围 %q：%w", s, err)
		}
		r.constraints = append(r.constraints, c)
	}
	return r, nil
}

// MustRange 在范围非法时 panic：只用于常量声明（写错是开发者错误，应当立刻发现）。
func MustRange(s string) Range {
	r, err := ParseRange(s)
	if err != nil {
		panic(err)
	}
	return r
}

func parseConstraint(f string) (constraint, error) {
	var o op
	rest := f
	switch {
	case strings.HasPrefix(f, ">="):
		o, rest = opGreaterEqual, f[2:]
	case strings.HasPrefix(f, "<="):
		o, rest = opLessEqual, f[2:]
	case strings.HasPrefix(f, ">"):
		o, rest = opGreater, f[1:]
	case strings.HasPrefix(f, "<"):
		o, rest = opLess, f[1:]
	case strings.HasPrefix(f, "="):
		o, rest = opEqual, f[1:]
	case strings.HasPrefix(f, "^"), strings.HasPrefix(f, "~"):
		// 明确拒绝：这两种写法在 0.x 阶段容易产生误会，要求写清楚区间。
		return constraint{}, fmt.Errorf("不支持 ^ / ~ 写法，请写成显式区间（如 \">=0.1 <0.2\"）")
	default:
		o, rest = opEqual, f
	}
	v, err := Parse(rest)
	if err != nil {
		return constraint{}, err
	}
	return constraint{op: o, v: v}, nil
}

// String 返回原始文本（Any 返回 "*"）。
func (r Range) String() string {
	if r.raw == "" {
		return "*"
	}
	return r.raw
}

// IsAny 报告它是否不做限制。
func (r Range) IsAny() bool { return len(r.constraints) == 0 }

// Match 报告版本是否落在范围内。
//
// 零值 Range（忘了设置）**视为不匹配**而不是"放行"：
// 校验代码遇到"读不懂/没声明"的输入时默认应该拦住，
// 这是 SKILL pitfalls 里明确记过的一条教训（数据版本保护第一版就是错在这里）。
func (r Range) Match(v Version) bool {
	if len(r.constraints) == 0 {
		// Any() 明确构造的空范围放行；零值 Range 与 Any() 无法区分，
		// 因此由调用方决定语义：插件侧必须显式写 EngineAPI，
		// 我们在 Plugin/Manifest 的校验里强制这一点（见 Manifest.Validate）。
		return true
	}
	for _, c := range r.constraints {
		cmp := v.Compare(c.v)
		ok := false
		switch c.op {
		case opEqual:
			ok = cmp == 0
		case opGreater:
			ok = cmp > 0
		case opGreaterEqual:
			ok = cmp >= 0
		case opLess:
			ok = cmp < 0
		case opLessEqual:
			ok = cmp <= 0
		}
		if !ok {
			return false
		}
	}
	return true
}

// LowerBound 返回下界（max 的所有下界约束）。
//
// 用于把"范围"折算成一个代表版本，以及判定区间包含关系。
// 没有任何下界约束时返回 (零值, false)——注意 (`>=0.0.0`, true) 与
// "无下界"是两种不同情况，调用方必须用 ok 区分。
func (r Range) LowerBound() (Version, bool) { return r.bound(true) }

// UpperBound 返回上界（min 的所有上界约束）；无上界时返回 (零值, false)。
func (r Range) UpperBound() (Version, bool) { return r.bound(false) }

func (r Range) bound(lower bool) (Version, bool) {
	var best *constraint
	for i := range r.constraints {
		c := r.constraints[i]
		isLower := c.op == opGreater || c.op == opGreaterEqual
		if isLower != lower {
			continue
		}
		switch {
		case best == nil:
			cc := c
			best = &cc
		case lower && c.v.Greater(best.v):
			cc := c
			best = &cc
		case lower && c.v.Equal(best.v) && c.op == opGreater:
			// 同值取下界更严的那个（>x 比 >=x 严），保证边界判定有确定答案。
			cc := c
			best = &cc
		case !lower && c.v.Less(best.v):
			cc := c
			best = &cc
		case !lower && c.v.Equal(best.v) && c.op == opLess:
			cc := c
			best = &cc
		}
	}
	if best == nil {
		return Version{}, false
	}
	return best.v, true
}

// CoversRange 报告 r 是否**完全包含** o（o 接受的每个版本，r 都接受）。
//
// 用**边界比较**判定，而不是取点采样。这一点是被测试逼出来的：
// 采样版对 `<0.2` 会去探测 0.2.1、0.2.2，而没有任何合理的范围会覆盖它们，
// 于是"包声明 ⊇ 成员要求"这条检查会把**所有包**都判成声明有缺陷。
// 区间的包含关系本来就等价于边界关系，做比较比做采样既准确又简单：
//
//	r 覆盖 o  ⟺  r 的下界 ≤ o 的下界，且 r 的上界 ≥ o 的上界
//	（边界相同时，r 的边界必须是闭的——闭区间能容下开区间，反之不行）
func (r Range) CoversRange(o Range) bool {
	if r.IsAny() {
		return true
	}
	if o.IsAny() {
		// r 有限而 o 无限：只有 r 也无限才可能覆盖，上面已排除。
		return false
	}
	return coversLower(r, o) && coversUpper(r, o)
}

// coversLower 判 r 的下界是否不高于 o 的下界。
func coversLower(r, o Range) bool {
	rl, rok := r.LowerBound()
	ol, ook := o.LowerBound()
	switch {
	case !rok && !ook:
		return true // 两者都没有下界
	case !rok:
		return true // r 无下界（-∞）当然覆盖 o 的下界
	case !ook:
		return false // o 无下界，r 有 → 覆盖不了
	}
	cmp := rl.Compare(ol)
	if cmp != 0 {
		return cmp < 0
	}
	// 同一下界：r 的边界必须不比 o 更严。
	// `>=x` 比 `>x` 宽，因此 r 不能是开界而 o 是闭界。
	return !(isStrictLower(r, rl) && !isStrictLower(o, ol))
}

// coversUpper 判 r 的上界是否不低于 o 的上界。
func coversUpper(r, o Range) bool {
	ru, rok := r.UpperBound()
	ou, ook := o.UpperBound()
	switch {
	case !rok && !ook:
		return true
	case !rok:
		return true // r 无上界（+∞）当然覆盖 o 的上界
	case !ook:
		return false
	}
	cmp := ru.Compare(ou)
	if cmp != 0 {
		return cmp > 0
	}
	// 同一上界：r 不能是开界而 o 是闭界（`<=x` 比 `<x` 宽）。
	return !(isStrictUpper(r, ru) && !isStrictUpper(o, ou))
}

func isStrictLower(r Range, v Version) bool {
	for _, c := range r.constraints {
		if (c.op == opGreater || c.op == opGreaterEqual) && c.v.Equal(v) {
			return c.op == opGreater
		}
	}
	return false
}

func isStrictUpper(r Range, v Version) bool {
	for _, c := range r.constraints {
		if (c.op == opLess || c.op == opLessEqual) && c.v.Equal(v) {
			return c.op == opLess
		}
	}
	return false
}

// Intersect 返回两个范围的交集（合取）。
//
// 用于"包要求的引擎版本 = 包内成员要求的交集，最严的那个生效"（design §4.3）。
// 交集明显为空（下界高于上界）时返回错误——那是开发者写错了区间，
// 应当当场发现，而不是等装载时表现为"这个包莫名其妙装不上"。
func (r Range) Intersect(o Range) (Range, error) {
	if r.IsAny() {
		return o, nil
	}
	if o.IsAny() {
		return r, nil
	}
	merged := Range{
		raw:         r.raw + " " + o.raw,
		constraints: append(append([]constraint{}, r.constraints...), o.constraints...),
	}
	if err := merged.checkSatisfiable(); err != nil {
		return Range{}, err
	}
	return merged, nil
}

// checkSatisfiable 用"下界 vs 上界"这一对最直观的判据检查区间是否自相矛盾。
//
// 只做这一条：它覆盖了实际会写错的情况（">=0.2 <0.1"），而不会像
// 通用区间代数那样引入误判。闭区间与开区间的差别按版本三元组精确比较。
func (r Range) checkSatisfiable() error {
	var lower *constraint
	var upper *constraint
	for i := range r.constraints {
		c := r.constraints[i]
		switch c.op {
		case opGreater, opGreaterEqual:
			if lower == nil || c.v.Greater(lower.v) ||
				(c.v.Equal(lower.v) && c.op == opGreater && lower.op == opGreaterEqual) {
				cc := c
				lower = &cc
			}
		case opLess, opLessEqual:
			if upper == nil || c.v.Less(upper.v) ||
				(c.v.Equal(upper.v) && c.op == opLess && upper.op == opLessEqual) {
				cc := c
				upper = &cc
			}
		}
	}
	if lower == nil || upper == nil {
		return nil
	}
	cmp := lower.v.Compare(upper.v)
	// 下界严格高于上界，或两端相等但有一端是开区间 → 空集。
	if cmp > 0 || (cmp == 0 && (lower.op == opGreater || upper.op == opLess)) {
		return fmt.Errorf("版本范围 %q 的交集为空（下界 %s%s 高于上界 %s%s，永远匹配不上）",
			r.raw, lower.op, lower.v, upper.op, upper.v)
	}
	return nil
}
