package geometry

import (
	"testing"
)

// TestRectNeverNegative 是这一层最核心的不变量：**任何变换都不产生负宽高**。
//
// 布局与画布都依赖它：如果 Rect 会出现负的 W/H，那么每个调用点都得自己判断，
// 而"某一处忘了判断"正是 v2.1.0 里栏位消失、右栏被挤爆那类问题的来源。
// 这里把尺寸网格穷举一遍，任何负数都是硬失败。
func TestRectNeverNegative(t *testing.T) {
	sizes := []int{-5, -1, 0, 1, 2, 3, 5, 17, 80, 200}
	rects := make([]Rect, 0, len(sizes)*len(sizes))
	for _, w := range sizes {
		for _, h := range sizes {
			rects = append(rects, NewRect(0, 0, w, h))
		}
	}

	check := func(t *testing.T, what string, r Rect) {
		t.Helper()
		if r.W < 0 || r.H < 0 {
			t.Fatalf("%s 产生了负宽高：%s", what, r)
		}
	}

	for _, r := range rects {
		check(t, "NewRect", r)
		check(t, "Inset", r.Inset(3, 2))
		check(t, "Inset(-1)", r.Inset(-1, -1))
		check(t, "Translate", r.Translate(-100, 50))
		check(t, "Intersect(空)", r.Intersect(Rect{}))
		check(t, "Union(空)", r.Union(Rect{}))
		check(t, "Row(越界)", r.Row(r.Y-1))
		check(t, "Row(越界下)", r.Row(r.Y1()+1))

		for _, n := range []int{-3, 0, 1, 4, 999} {
			top, rest := r.CutTop(n)
			check(t, "CutTop.top", top)
			check(t, "CutTop.rest", rest)
			restB, bot := r.CutBottom(n)
			check(t, "CutBottom.rest", restB)
			check(t, "CutBottom.bottom", bot)
			left, restL := r.CutLeft(n)
			check(t, "CutLeft.left", left)
			check(t, "CutLeft.rest", restL)
			restR, right := r.CutRight(n)
			check(t, "CutRight.rest", restR)
			check(t, "CutRight.right", right)

			// 切分必须"不重叠、不丢失"：两块面积之和恒等于原面积。
			if got := top.W*top.H + rest.W*rest.H; got != r.W*r.H {
				t.Fatalf("CutTop(%d) 面积和 %d != 原面积 %d（r=%s）", n, got, r.W*r.H, r)
			}
			if got := restB.W*restB.H + bot.W*bot.H; got != r.W*r.H {
				t.Fatalf("CutBottom(%d) 面积和 %d != 原面积 %d（r=%s）", n, got, r.W*r.H, r)
			}
			if got := left.W*left.H + restL.W*restL.H; got != r.W*r.H {
				t.Fatalf("CutLeft(%d) 面积和 %d != 原面积 %d（r=%s）", n, got, r.W*r.H, r)
			}
			if got := restR.W*restR.H + right.W*right.H; got != r.W*r.H {
				t.Fatalf("CutRight(%d) 面积和 %d != 原面积 %d（r=%s）", n, got, r.W*r.H, r)
			}
		}

		for _, lw := range []int{-1, 0, 18, 34, 200} {
			for _, rw := range []int{-1, 0, 16, 30, 200} {
				l, c, rr := r.SplitVertical(lw, rw)
				check(t, "SplitVertical.left", l)
				check(t, "SplitVertical.center", c)
				check(t, "SplitVertical.right", rr)
				// 三栏必须无缝铺满原宽度（这是"边框对齐"的前提）。
				if l.W+c.W+rr.W != r.W {
					t.Fatalf("SplitVertical(%d,%d) 宽度和 %d != %d（r=%s）",
						lw, rw, l.W+c.W+rr.W, r.W, r)
				}
				if l.X != r.X || c.X != l.X1() || rr.X != c.X1() {
					t.Fatalf("SplitVertical(%d,%d) 三栏未首尾相接：%s %s %s", lw, rw, l, c, rr)
				}
				if rr.X1() != r.X1() {
					t.Fatalf("SplitVertical(%d,%d) 右栏未对齐右边界：%s vs %s", lw, rw, rr, r)
				}
			}
		}
	}
}

// TestCutTopBottomOrder 固化"先剩下的、后被切走的"这一约定。
//
// 原型阶段真的踩过：底栏写成 CutTop 之后被撑成 22 行、主体只剩 1 行。
// 这条测试就是那个事故的墓碑。
func TestCutTopBottomOrder(t *testing.T) {
	full := NewRect(0, 0, 80, 24)

	header, rest := full.CutTop(1)
	if header.H != 1 || header.Y != 0 {
		t.Fatalf("CutTop 的第一个返回值应是顶部 1 行，实际 %s", header)
	}
	if rest.Y != 1 || rest.H != 23 {
		t.Fatalf("CutTop 的第二个返回值应是剩余（从第 1 行起 23 行），实际 %s", rest)
	}

	body, footer := rest.CutBottom(1)
	if body.Y != 1 || body.H != 22 {
		t.Fatalf("CutBottom 的第一个返回值应是剩余（主体 22 行），实际 %s", body)
	}
	if footer.Y != 23 || footer.H != 1 {
		t.Fatalf("CutBottom 的第二个返回值应是底部 1 行，实际 %s", footer)
	}

	// 三块拼起来正好铺满原矩形，不重不漏。
	if header.H+body.H+footer.H != full.H {
		t.Fatalf("header+body+footer 高度 %d != %d", header.H+body.H+footer.H, full.H)
	}
	if header.Y1() != body.Y {
		t.Fatalf("header 与 body 不相接：%d vs %d", header.Y1(), body.Y)
	}
	if body.Y1() != footer.Y {
		t.Fatalf("body 与 footer 不相接：%d vs %d", body.Y1(), footer.Y)
	}
}

// TestInsetDegenerate 验证"边框比内容还厚"时不出现负宽高。
func TestInsetDegenerate(t *testing.T) {
	r := NewRect(3, 4, 2, 2)
	got := r.Inset(2, 2)
	if !got.Empty() {
		t.Fatalf("2x2 收 2 列 2 行后应为空矩形，实际 %s", got)
	}
	// 位置仍应落在原矩形内（便于诊断信息可读）。
	if got.X != r.X+2 || got.Y != r.Y+2 {
		t.Fatalf("退化矩形的原点应在原矩形内缩后的位置，实际 %s", got)
	}
}

// TestContainsAndIntersect 覆盖包含与相交的边界（左闭右开）。
func TestContainsAndIntersect(t *testing.T) {
	r := NewRect(10, 5, 4, 3) // 覆盖 x∈[10,14) y∈[5,8)
	cases := []struct {
		x, y int
		want bool
	}{
		{10, 5, true},  // 左上角含
		{13, 7, true},  // 右下角含
		{14, 7, false}, // 右边界不含
		{13, 8, false}, // 下边界不含
		{9, 5, false},
		{10, 4, false},
	}
	for _, c := range cases {
		if got := r.Contains(c.x, c.y); got != c.want {
			t.Errorf("Contains(%d,%d) = %v，期望 %v", c.x, c.y, got, c.want)
		}
	}

	if got := r.Intersect(NewRect(0, 0, 0, 0)); got.W != 0 || got.H != 0 {
		t.Errorf("与空矩形相交应为空，实际 %s", got)
	}
	if got := r.Intersect(NewRect(12, 6, 10, 10)); got.String() != "(12,6 2x2)" {
		t.Errorf("相交结果应为 (12,6 2x2)，实际 %s", got)
	}
	if !r.ContainsRect(NewRect(11, 6, 2, 2)) {
		t.Error("内嵌小矩形应被判为被包含")
	}
	if r.ContainsRect(NewRect(13, 7, 4, 4)) {
		t.Error("越界矩形不应被判为被包含")
	}
	// 空矩形视为"在内部"：允许"没有内容要画"这种正常情况直接通过。
	if !r.ContainsRect(Rect{}) {
		t.Error("空矩形应视为被包含")
	}
}

// TestSplitVerticalShrinksFromRight 验证极小终端下三栏让位的顺序。
func TestSplitVerticalShrinksFromRight(t *testing.T) {
	// 只有 20 列，却要左 34 右 30：左栏拿满 20，中右归零，而不是出现负数。
	l, c, r := NewRect(0, 0, 20, 10).SplitVertical(34, 30)
	if l.W != 20 {
		t.Fatalf("左栏应拿满剩余宽度 20，实际 %d", l.W)
	}
	if c.W != 0 || r.W != 0 {
		t.Fatalf("中栏与右栏应为 0，实际 %d / %d", c.W, r.W)
	}
	if l.W+c.W+r.W != 20 {
		t.Fatalf("宽度和应为 20，实际 %d", l.W+c.W+r.W)
	}
}
