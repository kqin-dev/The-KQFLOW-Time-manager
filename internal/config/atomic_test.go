package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestReplaceFileRetriesWhileTargetLocked 验证“目标文件被占用时”改名会重试。
//
// 这是用户真实遇到的故障：界面上偶尔弹出
//
//	保存失败：替换 …\days\2026-10.json 失败：rename …tmpXXXX …
//
// 根因是 Windows 上 os.Rename 走 MoveFileEx(MOVEFILE_REPLACE_EXISTING)，
// 目标文件正被别的进程打开时直接返回 Access is denied。而这类锁几乎总是瞬时的
// （杀毒实时防护刚扫完、索引服务、同步盘读取），所以重试几次即可成功。
//
// 这里用 Windows 独占打开来制造真实的锁：在锁释放前 rename 必然失败，
// 释放后重试应当成功。
func TestReplaceFileRetriesWhileTargetLocked(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "2026-10.json")
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(dir, ".2026-10.json.tmp1")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 独占锁定目标文件（不共享读写），模拟被别的进程占用。
	lock, err := lockFileExclusive(dst)
	if err != nil {
		t.Skipf("本平台无法制造独占锁，跳过：%v", err)
	}

	// 200ms 后解锁；重试总预算约 550ms，应当能等到解锁后成功。
	go func() {
		time.Sleep(200 * time.Millisecond)
		lock.close()
	}()

	start := time.Now()
	if err := replaceFile(src, dst); err != nil {
		t.Fatalf("解锁后重试应当成功，实际失败：%v", err)
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Errorf("应当等待解锁后才成功，实际只用了 %v", elapsed)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("替换后内容应为 new，实际 %q", got)
	}
	// 临时文件应当已被改名消耗掉。
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("成功改名后临时文件不应残留")
	}
}

// TestReplaceFileGivesUpEventually 验证锁一直不放时会报错，而不是无限等待。
func TestReplaceFileGivesUpEventually(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "2026-10.json")
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, ".2026-10.json.tmp2")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	lock, err := lockFileExclusive(dst)
	if err != nil {
		t.Skipf("本平台无法制造独占锁，跳过：%v", err)
	}
	defer lock.close()

	start := time.Now()
	err = replaceFile(src, dst)
	if err == nil {
		t.Fatal("锁一直不放时应当报错")
	}
	if !isTransientRenameErr(err) {
		t.Errorf("该错误应被识别为可重试类型，实际：%v", err)
	}
	// 不应无限重试：总等待控制在 1 秒量级。
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("重试耗时过长：%v", elapsed)
	}
	// 目标文件必须保持原样，不能变成半个文件。
	//
	// 注意：这里不能用 os.ReadFile 校验——锁还在我们这个测试手里，
	// 自己去读同样会被拒绝（这正是 rename 失败的原因）。
	// 改用 Stat 看大小，它不需要读句柄。
	st, statErr := os.Stat(dst)
	if statErr != nil {
		t.Fatalf("目标文件应当仍然存在：%v", statErr)
	}
	if st.Size() != int64(len("old")) {
		t.Errorf("失败时原文件必须保持原样，实际大小 %d", st.Size())
	}
}

// TestReplaceFileErrorIsIdentified 验证 Windows 的“访问被拒绝”会被识别为可重试。
func TestReplaceFileErrorIsIdentified(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "2026-10.json")
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, ".2026-10.json.tmp3")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	lock, err := lockFileExclusive(dst)
	if err != nil {
		t.Skipf("本平台无法制造独占锁，跳过：%v", err)
	}
	defer lock.close()

	// 直接触发一次 rename，确认错误形态与用户看到的一致。
	rawErr := os.Rename(src, dst)
	if rawErr == nil {
		t.Fatal("被独占锁定时 rename 不应成功")
	}
	if !isTransientRenameErr(rawErr) {
		t.Errorf("Windows 的共享冲突应被识别为可重试，实际：%v", rawErr)
	}
}

// TestAtomicWriteReplacesExisting 验证原子写入的基本行为（回归保护）。
func TestAtomicWriteReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := AtomicWrite(path, []byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(path, []byte(`{"a":2}`)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"a":2}` {
		t.Errorf("应覆盖为最新内容，实际 %q", got)
	}
	// 目录里不该留下临时文件。
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "config.json" {
			t.Errorf("残留了额外文件：%s", e.Name())
		}
	}
}
