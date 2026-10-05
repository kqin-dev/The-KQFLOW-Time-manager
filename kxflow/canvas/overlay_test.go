package canvas

import (
	"testing"
)

// TestOverlaySuppressesCollisionOnlyWhileDeclared 验证 BeginOverlay 的开口
// **只在声明的范围内**生效。
//
// 这是给浮层留的唯一的门：提示条要就地盖掉上栏内容，那是设计而不是失误。
// 但门必须是可以关上的——否则"覆盖已有内容"这条最重要的不变量
// 就会被一次 defer 忘了恢复而整体失效。
func TestOverlaySuppressesCollisionOnlyWhileDeclared(t *testing.T) {
	c := New(20, 3)

	// 先写一层底。
	c.Text(0, 0, "底底底底底", 1)
	if c.Diag.Collisions != 0 {
		t.Fatalf("首次写入不该有冲突，实际 %d", c.Diag.Collisions)
	}

	// ① 声明为浮层后覆盖：不算冲突。
	restore := c.BeginOverlay()
	c.Text(0, 0, "浮层", 1)
	if !c.Overlaying() {
		t.Fatal("BeginOverlay 之后应当处于浮层状态")
	}
	if c.Diag.Collisions != 0 {
		t.Errorf("声明为浮层时覆盖不该记冲突，实际 %d", c.Diag.Collisions)
	}
	restore()

	// ② 恢复之后覆盖：必须照旧报冲突（这就是"门关上了"）。
	if c.Overlaying() {
		t.Fatal("restore 之后不该还处于浮层状态")
	}
	before := c.Diag.Collisions
	c.Text(0, 0, "再盖", 1)
	if c.Diag.Collisions == before {
		t.Error("恢复之后覆盖必须重新记冲突——否则这条不变量形同虚设")
	}
}

// TestOverlayIsNested 验证浮层状态是**可嵌套且能正确恢复**的。
//
// 用 prev 而不是布尔量：嵌套的浮层（提示条叠在弹窗上）恢复外层时
// 必须回到"外层仍在浮层中"，而不是一律置回 false。
func TestOverlayIsNested(t *testing.T) {
	c := New(10, 1)
	outer := c.BeginOverlay()
	inner := c.BeginOverlay()
	inner()
	if !c.Overlaying() {
		t.Error("内层恢复后应回到外层的浮层状态")
	}
	outer()
	if c.Overlaying() {
		t.Error("外层恢复后应退出浮层状态")
	}
}
