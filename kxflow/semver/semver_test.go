package semver

import "testing"

// TestParseAndCompare 覆盖版本解析与比较。
func TestParseAndCompare(t *testing.T) {
	cases := []struct {
		in            string
		want          Version
		wantErr       bool
		allowTwoParts bool // "0.1" 这种两段写法应当可解析
	}{
		{in: "0.1.0", want: New(0, 1, 0)},
		{in: "v1.2.3", want: New(1, 2, 3)},
		{in: "0.1", want: New(0, 1, 0), allowTwoParts: true},
		{in: "2", want: New(2, 0, 0), allowTwoParts: true},
		{in: " 1.0.0 ", want: New(1, 0, 0)},
		{in: "1.2.3.4", wantErr: true},
		{in: "", wantErr: true},
		{in: "a.b.c", wantErr: true},
		{in: "-1.0.0", wantErr: true},
		{in: "1..0", wantErr: true},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("Parse(%q) 应当报错，实际得到 %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q) 意外报错：%v", c.in, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("Parse(%q) = %v，期望 %v", c.in, got, c.want)
		}
	}

	if !New(0, 1, 0).Less(New(0, 2, 0)) {
		t.Error("0.1.0 应小于 0.2.0")
	}
	if New(1, 0, 0).Compare(New(1, 0, 0)) != 0 {
		t.Error("相同版本应比较为 0")
	}
	if New(1, 0, 1).Compare(New(1, 0, 0)) != 1 {
		t.Error("patch 更大应比较为 1")
	}

	// Compatible：0.x 阶段 minor 即破坏性位。
	if !New(0, 1, 5).Compatible(New(0, 1, 0)) {
		t.Error("0.1.5 与 0.1.0 应兼容")
	}
	if New(0, 1, 0).Compatible(New(0, 2, 0)) {
		t.Error("0.1.0 与 0.2.0 不应兼容（0.x 的 minor 是破坏性位）")
	}
	if !New(1, 5, 0).Compatible(New(1, 9, 0)) {
		t.Error("1.x 内部应兼容")
	}
	if New(1, 0, 0).Compatible(New(2, 0, 0)) {
		t.Error("跨主版本不应兼容")
	}
}

// TestRangeMatch 覆盖各种范围写法。
func TestRangeMatch(t *testing.T) {
	cases := []struct {
		rng   string
		v     string
		match bool
	}{
		{">=0.1 <0.2", "0.1.0", true},
		{">=0.1 <0.2", "0.1.9", true},
		{">=0.1 <0.2", "0.2.0", false},
		{">=0.1 <0.2", "0.0.9", false},
		{"0.1.0", "0.1.0", true},
		{"0.1.0", "0.1.1", false},
		{"=0.1.0", "0.1.0", true},
		{">0.1.0", "0.1.0", false},
		{">0.1.0", "0.1.1", true},
		{"<=0.2.0", "0.2.0", true},
		{"<=0.2.0", "0.2.1", false},
		{"<1.0.0", "0.9.9", true},
		{"<1.0.0", "1.0.0", false},
		{"", "9.9.9", true}, // 不限制
	}
	for _, c := range cases {
		r := MustRange(c.rng)
		got := r.Match(MustParse(c.v))
		if got != c.match {
			t.Errorf("%q.Match(%q) = %v，期望 %v", c.rng, c.v, got, c.match)
		}
	}
}

// TestParseRangeRejects 覆盖应当被拒绝的范围写法。
//
// 拒绝 `^` / `~` / `||` 是刻意的：插件的 EngineAPI 应当是一条清晰的区间，
// 否则"为什么它装上了/没装上"会变得难解释。
func TestParseRangeRejects(t *testing.T) {
	bad := []string{">=0.1 || <0.5", "^0.1.0", "~0.1.0", ">=", "abc", ">=x.y.z"}
	for _, s := range bad {
		if _, err := ParseRange(s); err == nil {
			t.Errorf("ParseRange(%q) 应当报错", s)
		}
	}
}

// TestIntersect 覆盖交集，以及"空交集要当场发现"。
func TestIntersect(t *testing.T) {
	a := MustRange(">=0.1 <0.3")
	b := MustRange(">=0.2 <0.4")
	got, err := a.Intersect(b)
	if err != nil {
		t.Fatalf("交集不应报错：%v", err)
	}
	if !got.Match(MustParse("0.2.0")) || got.Match(MustParse("0.1.0")) || got.Match(MustParse("0.3.0")) {
		t.Errorf("交集应只接受 [0.2,0.3)，实际 %q 的匹配行为不符", got)
	}

	// Any 与任何范围的交集就是那个范围。
	any := Any()
	if merged, err := any.Intersect(a); err != nil || merged.String() != a.String() {
		t.Errorf("Any 与 a 的交集应为 a，实际 %v / %v", merged, err)
	}

	// 下界高于上界：开发者写错了，必须当场报错。
	if _, err := MustRange(">=0.5").Intersect(MustRange("<0.2")); err == nil {
		t.Error("下界高于上界的交集应报错")
	}
	// 相同边界的开闭冲突（>=x 与 <x）也是空集。
	if _, err := MustRange(">=0.2").Intersect(MustRange("<0.2")); err == nil {
		t.Error(">=0.2 与 <0.2 的交集为空，应报错")
	}
	// 合法紧邻区间不应误报：>=0.1 <0.2 自身与自身相交必须成功。
	if _, err := MustRange(">=0.1 <0.2").Intersect(MustRange(">=0.1 <0.2")); err != nil {
		t.Errorf("相同区间相交不应报错：%v", err)
	}
}

// TestCoversRange 是"包声明必须覆盖成员要求"这条规则的判定核心。
//
// 它曾经用取点采样实现，结果 `<0.2` 会去探测 0.2.1、0.2.2，
// 于是**所有包**都被判成声明有缺陷。改成边界比较之后才正确。
// 这条测试就是那次事故的墓碑。
func TestCoversRange(t *testing.T) {
	cases := []struct {
		outer, inner string
		want         bool
		why          string
	}{
		{">=0.1 <0.2", ">=0.1 <0.2", true, "完全相同"},
		{">=0.1 <0.3", ">=0.1 <0.2", true, "成员更窄（更保守），合法"},
		{">=0.1 <0.2", ">=0.1 <0.3", false, "成员要求超出包承诺"},
		{">=0.1 <0.2", ">=0.2 <0.3", false, "成员下界超出包上界"},
		{">=0.1", ">=0.1 <0.2", true, "包无上界，覆盖一切上界"},
		{"<0.5", ">=0.1 <0.2", true, "包无下界，覆盖一切下界"},
		{">=0.1 <0.2", ">=0.1", false, "成员无上界而包有上界"},
		{">=0.1 <0.2", "", false, "成员不限制而包有限制"},
		{"", ">=0.1 <0.2", true, "包不限制"},
		// 开闭边界的严格性：闭区间能容下开区间，反之不行。
		{">=0.1 <0.2", ">0.1 <0.2", true, "包下界更宽"},
		{">0.1 <0.2", ">=0.1 <0.2", false, "包下界更严，容不下"},
		{"<=0.2", "<0.2", true, "包上界更宽"},
		{"<0.2", "<=0.2", false, "包上界更严，容不下"},
	}
	for _, c := range cases {
		got := MustRange(c.outer).CoversRange(MustRange(c.inner))
		if got != c.want {
			t.Errorf("%q.CoversRange(%q) = %v，期望 %v（%s）",
				c.outer, c.inner, got, c.want, c.why)
		}
	}
}

// TestBounds 覆盖上下界查询（开闭边界的取舍要确定）。
func TestBounds(t *testing.T) {
	if _, ok := Any().LowerBound(); ok {
		t.Error("不限范围不应有下界")
	}
	if v, ok := MustRange(">=0.1 <0.3").LowerBound(); !ok || !v.Equal(New(0, 1, 0)) {
		t.Errorf("下界应为 0.1.0，实际 %v/%v", v, ok)
	}
	if v, ok := MustRange(">=0.1 <0.3").UpperBound(); !ok || !v.Equal(New(0, 3, 0)) {
		t.Errorf("上界应为 0.3.0，实际 %v/%v", v, ok)
	}
	// 同值时取更严的那个，保证边界判定有确定答案。
	if v, _ := MustRange(">=0.1 >0.2").LowerBound(); !v.Equal(New(0, 2, 0)) {
		t.Errorf("下界应取更严的 0.2.0，实际 %v", v)
	}
	if v, _ := MustRange("<=0.3 <0.2").UpperBound(); !v.Equal(New(0, 2, 0)) {
		t.Errorf("上界应取更严的 0.2.0，实际 %v", v)
	}
}
