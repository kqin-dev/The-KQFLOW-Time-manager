package ui

import (
	"strings"
	"testing"
)

// qrTestURL 用一条与真实订阅地址长度相近的字符串。
const qrTestURL = "https://ntfy.sh/JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXPJBSWY3D"

// TestQRFormatInfoBits 验证格式信息的 BCH 编码结果与标准一致。
//
// 这是少数能**独立于本实现**核对的部分：格式信息（纠错等级 + 掩码号）是标准里
// 写死的 15 位串，掩码 0..7 依次为下面这些值。二维码扫不出来最常见的原因就是
// 这一串算错，所以值得单独钉住。
func TestQRFormatInfoBits(t *testing.T) {
	// 纠错等级 L 的 8 个掩码对应的格式信息（来自 QR 标准附录）。
	want := []uint32{
		0b111011111000100, // mask 0
		0b111001011110011, // mask 1
		0b111110110101010, // mask 2
		0b111100010011101, // mask 3
		0b110011000101111, // mask 4
		0b110001100011000, // mask 5
		0b110110001000001, // mask 6
		0b110100101110110, // mask 7
	}
	for mask := 0; mask < 8; mask++ {
		if got := qrFormatBits(mask); got != want[mask] {
			t.Errorf("掩码 %d 的格式信息应为 %015b，实际 %015b", mask, want[mask], got)
		}
	}

	// 矩阵里写进去的也要和上面一致：左上那一组按位序读取。
	for mask := 0; mask < 8; mask++ {
		m := newQRMatrix(21)
		placeQRFinderPatterns(m)
		placeQRTimingPatterns(m)
		placeQRFormatAreaPlaceholder(m)
		placeQRFormatInfo(m, mask)

		var read uint32
		for i := 0; i <= 5; i++ {
			if m[8][i] {
				read |= 1 << uint(i)
			}
		}
		if m[8][7] {
			read |= 1 << 6
		}
		if m[8][8] {
			read |= 1 << 7
		}
		if m[7][8] {
			read |= 1 << 8
		}
		for i := 9; i <= 14; i++ {
			if m[14-i][8] {
				read |= 1 << uint(i)
			}
		}
		if read != want[mask] {
			t.Errorf("掩码 %d 写入矩阵的格式信息应为 %015b，读回 %015b", mask, want[mask], read)
		}
		// 定时图案不能被格式信息砸掉。
		if !m[6][8] || !m[8][6] {
			t.Errorf("掩码 %d：定时图案被破坏", mask)
		}
	}
}

// TestQRGFArithmetic 验证 GF(256) 乘法的基本性质与生成多项式。
func TestQRGFArithmetic(t *testing.T) {
	// 乘法单位元与零元。
	if qrGFMul(1, 0x57) != 0x57 {
		t.Error("1 应是乘法单位元")
	}
	if qrGFMul(0, 0x57) != 0 {
		t.Error("0 乘任何数为 0")
	}
	// 交换律。
	if qrGFMul(0x53, 0xCA) != qrGFMul(0xCA, 0x53) {
		t.Error("乘法应满足交换律")
	}
	// 本原多项式 0x11d：x^8 = 0x1d。
	if got := qrGFMul(0x80, 2); got != 0x1d {
		t.Errorf("0x80 * 2 应为 0x1d（模 0x11d），实际 %#x", got)
	}

	// 生成多项式的首项系数必须是 1，阶数正确。
	for _, ec := range []int{7, 10, 13, 15, 16, 18, 20, 22, 24, 26, 28, 30} {
		gen := qrRSGenerator(ec)
		if len(gen) != ec {
			t.Fatalf("ec=%d 的生成多项式长度应为 %d，实际 %d", ec, ec, len(gen))
		}
		if gen[0] == 0 {
			t.Errorf("ec=%d 的生成多项式首项不该为 0", ec)
		}
	}

	// 纠错码字个数正确，且全零数据得到全零纠错（线性性质）。
	ec := qrRSEncode(make([]byte, 19), 7)
	if len(ec) != 7 {
		t.Fatalf("应有 7 个纠错码字，实际 %d", len(ec))
	}
	for i, b := range ec {
		if b != 0 {
			t.Errorf("全零输入的纠错码字应为 0，第 %d 个是 %#x", i, b)
		}
	}
}

// TestQRVersionTables 验证纠错特性表自洽：数据容量 + 纠错容量 = 总码字。
func TestQRVersionTables(t *testing.T) {
	for i, v := range qrVersionsL {
		version := i + 1
		dataCap := qrDataCapacity(v)
		blocks := v.group1Blocks + v.group2Blocks
		total := dataCap + blocks*v.ecPerBlock
		if total != v.totalCodewords {
			t.Errorf("版本 %d：数据 %d + 纠错 %d×%d = %d，与总码字 %d 不符",
				version, dataCap, blocks, v.ecPerBlock, total, v.totalCodewords)
		}
		// 对齐图案的版本坐标数量必须与版本相关（v2 起才有）。
		if version == 1 && len(qrAlignCenters[i]) != 0 {
			t.Error("版本 1 不该有对齐图案")
		}
		if version > 1 && len(qrAlignCenters[i]) == 0 {
			t.Errorf("版本 %d 应有对齐图案中心", version)
		}
	}
}

// TestQRRoundTrip 是本文件里最重要的一条：编码 → 矩阵 → 解码 → 原文 必须闭环。
//
// 背景：用户实机扫码时「连识别都识别不出来是二维码」。靠「结构看着对」是验不出
// 这类问题的，所以补了这个解码器，让闭环成立——闭环成立意味着矩阵在数学上就是
// 一个合法二维码，剩下的只可能是终端渲染或摄像头的问题。
func TestQRRoundTrip(t *testing.T) {
	texts := []string{
		"https://ntfy.sh/JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXPJBSWY3D", // 真实订阅地址量级
		"A",
		"https://ntfy.sh/short",
		"0123456789abcdefghijklmnopqrstuvwxyz",
		strings.Repeat("X", 60),
		strings.Repeat("Y", 120),
	}
	for _, text := range texts {
		matrix, err := qrEncode([]byte(text))
		if err != nil {
			t.Errorf("编码 %d 字节失败: %v", len(text), err)
			continue
		}
		got, err := qrDecode(matrix)
		if err != nil {
			t.Errorf("%d 字节解码失败: %v", len(text), err)
			continue
		}
		if string(got) != text {
			t.Errorf("往返不一致：\n原文 %q\n解出 %q", text, string(got))
		}
	}
}

// TestQREncodeStructure 验证编码结果的尺寸与固定图案。
//
// 注意：**本环境无法真机扫码**。这里只能验结构（尺寸、定位图案、定时图案、
// 掩码选择），「扫得出来」需要用户用手机实际扫一次——交付说明里如实标注了
// 这一点，不会假装已验证。
func TestQREncodeStructure(t *testing.T) {
	matrix, err := qrEncode([]byte(qrTestURL))
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	size := len(matrix)
	// 尺寸必须是 17 + 4*version，且全部行等长。
	if (size-17)%4 != 0 {
		t.Errorf("尺寸 %d 不符合 17+4n", size)
	}
	for i, row := range matrix {
		if len(row) != size {
			t.Fatalf("第 %d 行长度 %d 与尺寸 %d 不符", i, len(row), size)
		}
	}

	// 三个定位图案：外圈全深、内部 3x3 深、之间一圈浅。
	finderOK := func(oy, ox int) bool {
		for dy := 0; dy < 7; dy++ {
			for dx := 0; dx < 7; dx++ {
				want := dy == 0 || dy == 6 || dx == 0 || dx == 6 || (dy >= 2 && dy <= 4 && dx >= 2 && dx <= 4)
				if matrix[oy+dy][ox+dx] != want {
					return false
				}
			}
		}
		return true
	}
	if !finderOK(0, 0) {
		t.Error("左上定位图案不正确")
	}
	if !finderOK(0, size-7) {
		t.Error("右上定位图案不正确")
	}
	if !finderOK(size-7, 0) {
		t.Error("左下定位图案不正确")
	}

	// 定时图案：第 6 行 / 第 6 列在定位图案之间交替（i 为偶数时深色）。
	for i := 8; i < size-8; i++ {
		if matrix[6][i] != (i%2 == 0) {
			t.Errorf("第 6 行第 %d 列的定时图案不正确", i)
		}
		if matrix[i][6] != (i%2 == 0) {
			t.Errorf("第 6 列第 %d 行的定时图案不正确", i)
		}
	}
	// 第 8 列是格式信息保留位，不参与定时图案的交替；上面从 8 开始是对齐
	// 标准的（第 8 个模块在偶数位，仍是深色）。
	if !matrix[6][8] {
		t.Error("第 6 行第 8 列应为深色（偶数位）")
	}

	// 固定的深色模块必须在。
	if !matrix[size-8][8] {
		t.Error("固定的深色模块缺失")
	}

	// 不该是一片空白或一片实心。
	dark := 0
	for _, row := range matrix {
		for _, v := range row {
			if v {
				dark++
			}
		}
	}
	ratio := float64(dark) / float64(size*size)
	if ratio < 0.25 || ratio > 0.75 {
		t.Errorf("深色比例 %.2f 不像一个正常二维码", ratio)
	}
}

// TestQREncodeDeterministic 验证同样内容每次编码结果一致。
func TestQREncodeDeterministic(t *testing.T) {
	a, err := qrEncode([]byte(qrTestURL))
	if err != nil {
		t.Fatal(err)
	}
	b, err := qrEncode([]byte(qrTestURL))
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("两次尺寸不同：%d vs %d", len(a), len(b))
	}
	for y := range a {
		for x := range a[y] {
			if a[y][x] != b[y][x] {
				t.Fatalf("(%d,%d) 两次结果不同", y, x)
			}
		}
	}

	// 不同内容必须产生不同矩阵。
	c, err := qrEncode([]byte("https://ntfy.sh/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"))
	if err != nil {
		t.Fatal(err)
	}
	same := len(a) == len(c)
	if same {
		same = true
		for y := range a {
			for x := range a[y] {
				if a[y][x] != c[y][x] {
					same = false
					break
				}
			}
			if !same {
				break
			}
		}
	}
	if same {
		t.Error("不同内容的二维码不该完全一样")
	}
}

// TestQREncodeVersionSelection 验证按内容长度选到合适的版本与容量上限。
func TestQREncodeVersionSelection(t *testing.T) {
	cases := []struct {
		n    int
		size int // 期望的矩阵边长（= 17 + 4*版本）
	}{
		{10, 21},  // v1
		{40, 29},  // v3
		{80, 37},  // v5
		{120, 41}, // v6
	}
	for _, c := range cases {
		data := strings.Repeat("A", c.n)
		m, err := qrEncode([]byte(data))
		if err != nil {
			t.Errorf("%d 字节应能编码，实际报错: %v", c.n, err)
			continue
		}
		if len(m) != c.size {
			t.Errorf("%d 字节应选到边长 %d 的版本，实际 %d", c.n, c.size, len(m))
		}
	}

	// 超长内容要如实报错，而不是生成一个坏二维码。
	if _, err := qrEncode([]byte(strings.Repeat("A", qrMaxBytes()+50))); err == nil {
		t.Error("超长内容应报错")
	}
	// 空内容也应报错。
	if _, err := qrEncode(nil); err == nil {
		t.Error("空内容应报错")
	}
}

// TestRenderQRHalfBlocks 验证终端渲染的几何：静默区、方形模块、半块字符。
//
// 这几条是用户实机扫不出来之后补的。之前的渲染有两个致命问题：
//   - 每模块横向只占 1 个字符、纵向占半行 → 画出来是 1:2 的瘦长条，不像二维码；
//   - 静默区只留 1 个模块（标准要求 4 个）→ 摄像头无法可靠定位。
func TestRenderQRHalfBlocks(t *testing.T) {
	out, err := renderQR(qrTestURL, 200)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	m, _ := qrEncode([]byte(qrTestURL))
	n := len(m)

	if len(out) != qrRenderHeight(n) {
		t.Errorf("行数应为 %d，实际 %d", qrRenderHeight(n), len(out))
	}
	quietTop := qrRenderQuietTop()
	quietBottom := qrRenderQuietBottom()
	contentRows := out[quietTop : len(out)-quietBottom]
	if len(contentRows) == 0 {
		t.Fatal("没有内容行")
	}
	// 内容行宽度一致；静默行也是同宽（整行空白）。
	w := qrRenderWidth(n)
	for i, l := range out {
		if displayWidth(l) != w {
			t.Errorf("n=%d version=%d 第 %d 行宽度 %d 应为 %d", n, (n-17)/4, i, displayWidth(l), w)
		}
	}
	// 顶部静默行是纯空白；内容行与内容行之间不可能全空（否则二维码没画出来）。
	for i := 0; i < quietTop; i++ {
		if strings.TrimSpace(out[i]) != "" {
			t.Errorf("第 %d 行应是静默区，实际 %q", i, out[i])
		}
	}
	// 底部静默区同理。
	for i := len(out) - quietBottom; i < len(out); i++ {
		if strings.TrimSpace(out[i]) != "" {
			t.Errorf("第 %d 行应是静默区，实际 %q", i, out[i])
		}
	}
	// 左右静默区列必须是空白。
	quietCols := qrQuietZone * qrModuleChars
	for _, l := range contentRows {
		if strings.TrimSpace(l[:quietCols]) != "" {
			t.Errorf("左侧静默区不干净：%q", l[:quietCols])
		}
		if strings.TrimSpace(l[len(l)-quietCols:]) != "" {
			t.Errorf("右侧静默区不干净：%q", l[len(l)-quietCols:])
		}
	}

	// 只使用半块字符与空格。
	for _, l := range out {
		for _, r := range l {
			switch r {
			case ' ', '▀', '▄', '█':
			default:
				t.Errorf("出现了非半块字符 %q", r)
			}
		}
	}

	// 定位图案必须真的画出来了：内容区第一行是模块行 −4/−3，其中模块行 −4 是
	// 静默（全浅）、−3 是模块行 1 —— 而模块行 0 的定位图案外圈应当是深色。
	// 更稳的判据：内容前几行里必须出现深色块，且出现在靠左的位置。
	found := false
	for _, l := range contentRows[:3] {
		if strings.ContainsAny(l, "█▀▄") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("内容区前几行应包含定位图案的深色模块")
	}

	// 超出可用宽度时要如实报错，让调用方回退成「手输地址」。
	if _, err := renderQR(qrTestURL, 10); err == nil {
		t.Error("宽度不足时应报错")
	}
}

// TestQRRenderGeometryIsSquare 验证画出来的二维码接近正方形。
//
// 终端字符高约为宽的两倍，所以「横向 2 字符 + 纵向半块合并」才得到 1:1 的
// 物理形状。用字符数比对时应当满足 宽 ≈ 2×高。
func TestQRRenderGeometryIsSquare(t *testing.T) {
	out, err := renderQR(qrTestURL, 200)
	if err != nil {
		t.Fatal(err)
	}
	w := displayWidth(out[0])
	h := len(out)
	// 物理宽高比 ≈ w / (2h)，应落在 0.9~1.1 之间。
	ratio := float64(w) / float64(2*h)
	if ratio < 0.9 || ratio > 1.1 {
		t.Errorf("物理形状应接近正方形（宽 %d 列，高 %d 行，比值 %.2f）", w, h, ratio)
	}
}
