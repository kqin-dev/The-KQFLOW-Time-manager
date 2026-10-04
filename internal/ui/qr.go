package ui

import (
	"fmt"
	"strings"
)

// 精简 QR 编码器（见需求 3 的二维码要求）。
//
// 为什么自己写而不是引第三方库：这个二维码只用于「手机扫码订阅一个地址」，
// 引入一个完整 QR 库（几百 KB 的依赖、还要进用户的 go.mod）不成比例；而且
// ntfy 地址很短，只需要一个很小的子集。
//
// 实现范围（够用即可，不做通用库）：
//   - 字节模式（UTF-8 直接放进去，ntfy 地址是 ASCII）
//   - 纠错等级 L（容量最大，地址这种短文本不需要更强的纠错）
//   - 版本 1-10（最多 271 字节，远超一个订阅地址）
//   - 8 种掩码全部计算并按标准罚分选最优
//
// 表格数字来自 QR 标准（ISO/IEC 18004）的纠错特性表，这里只裁剪到用得到的版本。

// qrECBlock 描述一个版本在 L 级下的分块方式。
type qrECBlock struct {
	// totalCodewords 是该版本的数据区总码字数。
	totalCodewords int
	// ecPerBlock 是每个纠错块里纠错码字的个数。
	ecPerBlock int
	// group1Blocks / group1Data 是第一组的块数与每块数据码字数。
	group1Blocks int
	group1Data   int
	// group2Blocks 是第二组的块数；group2Data = group1Data + 1。
	group2Blocks int
}

// qrVersionsL 是版本 1-10 在纠错等级 L 下的参数。
var qrVersionsL = []qrECBlock{
	{26, 7, 1, 19, 0},    // v1
	{44, 10, 1, 34, 0},   // v2
	{70, 15, 1, 55, 0},   // v3
	{100, 20, 1, 80, 0},  // v4
	{134, 26, 1, 108, 0}, // v5
	{172, 18, 2, 68, 0},  // v6
	{196, 20, 2, 78, 0},  // v7
	{242, 24, 2, 97, 0},  // v8
	{292, 30, 2, 116, 0}, // v9
	{346, 18, 2, 68, 2},  // v10
}

// qrAlignCenters 是各版本的对齐图案中心坐标。
var qrAlignCenters = [][]int{
	{},          // v1 没有
	{6, 18},     // v2
	{6, 22},     // v3
	{6, 26},     // v4
	{6, 30},     // v5
	{6, 34},     // v6
	{6, 22, 38}, // v7
	{6, 24, 42}, // v8
	{6, 26, 46}, // v9
	{6, 28, 50}, // v10
}

// qrQuietZone 是静默区宽度（模块数）。
//
// **标准要求 4 个模块**，这不是可选的：静默区不足时手机摄像头无法可靠地定位
// 三个定位图案。用户实机扫码失败后复查发现这里曾经只留了 1 个模块。
const qrQuietZone = 4

// qrModuleChars 是每个模块横向占用的字符数。
//
// **必须是 1**。终端字符大致是「宽 1 : 高 2」，而一个字符行通过半块字符能表示
// **两个**模块行。所以「横向 1 个字符 + 纵向半行」才是 1:1 的物理方形。
//
// 曾经设成 2（以为要"补上"高度），结果横向 2 字符 + 纵向半行 = 4:1 —— 二维码
// 在终端里被拉成两倍宽，手机完全识别不出是二维码。这正是用户实机反馈的根因。
const qrModuleChars = 1

// QRMaxBytes 报告本编码器能容纳的最大字节数。
func qrMaxBytes() int {
	last := qrVersionsL[len(qrVersionsL)-1]
	return last.totalCodewords - 3 // 留出模式指示与长度字段的余量
}

// qrDataCapacity 返回某版本在 L 级下可用的数据码字数。
func qrDataCapacity(v qrECBlock) int {
	return v.group1Blocks*v.group1Data + v.group2Blocks*(v.group1Data+1)
}

// qrRenderWidth 返回把模块数 n 画出来需要的字符列数（含静默区）。
//
// 与 renderQR 里的 contentWidth 必须是同一个算式：静默边距 + [静默区 → 模块区
// → 静默区] + 静默边距 = (n + 4*静默区) 个模块。
func qrRenderWidth(n int) int {
	return (n + 4*qrQuietZone) * qrModuleChars
}

// qrRenderHeight 返回把模块数 n 画出来需要的字符行数（含静默区）。
//
// 内容部分从模块行 −静默区 开始、每次吃掉两个模块行，共 (n+2*静默区+1)/2 行
// （模块行数为奇数时最后多出半行）；上下再各加 qrRenderQuietTop/Bottom 行
// 纯空白。三者必须与 renderQR 里的循环完全一致，否则测试会拿错误的行数断言。
func qrRenderHeight(n int) int {
	return (n+2*qrQuietZone+1)/2 + qrRenderQuietTop() + qrRenderQuietBottom()
}

// qrRenderQuietTop 返回顶部静默占用的字符行数。
func qrRenderQuietTop() int { return 2 }

// qrRenderQuietBottom 返回底部静默占用的字符行数。
func qrRenderQuietBottom() int { return 2 }

// renderQR 把文本编码成二维码，用半块字符画出来。
//
// 布局（这是能在终端里被扫出来的关键）：
//   - 每个模块横向 1 个字符（见 qrModuleChars）。
//   - 每个字符行表示纵向 2 个模块（上半个用 ▀，下半个用 ▄，两个都深用 █）。
//     两者合起来才让模块在终端里是方的。
//   - 四周留够 4 个模块的静默区（标准要求，不是可选项）。
//
// maxWidth 是可用字符列数；放不下时返回错误，由调用方回退成「手输地址」。
func renderQR(text string, maxWidth int) ([]string, error) {
	matrix, err := qrEncode([]byte(text))
	if err != nil {
		return nil, err
	}
	n := len(matrix)
	width := qrRenderWidth(n)
	if maxWidth > 0 && width > maxWidth {
		return nil, fmt.Errorf("二维码需要 %d 列，可用 %d 列", width, maxWidth)
	}

	quietCols := qrQuietZone * qrModuleChars
	blank := strings.Repeat(" ", quietCols)
	quietRows := qrRenderQuietTop()
	// 内容行宽度必须与 qrRenderWidth 用同一个算式，否则空白行与内容行宽度不一致。
	//
	// 一行的组成是：静默边距 + [静默区 → 模块区 → 静默区] + 静默边距。
	// 中括号里的范围已经是 n + 2*静默区 个模块，两侧还要各再留 qrQuietZone 个
	// 模块的边距，所以总宽是 (n + 4*静默区) * 每模块字符数。
	// 曾经写成 (n + 2*静默区) * 每模块字符数，结果静默行 82 列、内容行 98 列，
	// 二维码在终端里左右被裁掉——这正是手机认不出来的原因之一。
	contentWidth := (n + 4*qrQuietZone) * qrModuleChars

	// 取模块值：越界（静默区）一律视为浅色。
	dark := func(y, x int) bool {
		if y < 0 || y >= n || x < 0 || x >= n {
			return false
		}
		return matrix[y][x]
	}

	var out []string
	// 顶部静默区：整行空白，宽度与内容行一致。
	for i := 0; i < quietRows; i++ {
		out = append(out, strings.Repeat(" ", contentWidth))
	}
	// 内容：两行模块合成一行字符。
	for top := -qrQuietZone; top < n+qrQuietZone; top += 2 {
		var b strings.Builder
		b.WriteString(blank)
		for x := -qrQuietZone; x < n+qrQuietZone; x++ {
			up := dark(top, x)
			down := dark(top+1, x)
			ch := " "
			switch {
			case up && down:
				ch = "█"
			case up:
				ch = "▀"
			case down:
				ch = "▄"
			}
			// 横向重复：保证模块是方的。
			for i := 0; i < qrModuleChars; i++ {
				b.WriteString(ch)
			}
		}
		b.WriteString(blank)
		out = append(out, b.String())
	}
	// 底部静默区。
	for i := 0; i < qrRenderQuietBottom(); i++ {
		out = append(out, strings.Repeat(" ", contentWidth))
	}
	return out, nil
}

// qrEncode 生成二维码模块矩阵；true 表示深色模块。
func qrEncode(data []byte) ([][]bool, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("二维码内容为空")
	}
	// 1) 选版本与分块参数。
	version := -1
	var spec qrECBlock
	for i, v := range qrVersionsL {
		// 数据位 = 4（模式）+ 长度字段 + 8*字节数；再换算成码字。
		lenBits := 8
		if i+1 >= 10 {
			lenBits = 16
		}
		needBits := 4 + lenBits + 8*len(data)
		needCodewords := (needBits + 7) / 8
		if needCodewords <= qrDataCapacity(v) {
			version, spec = i+1, v
			break
		}
	}
	if version < 0 {
		return nil, fmt.Errorf("内容过长（%d 字节，上限约 %d）", len(data), qrMaxBytes())
	}

	// 2) 组装数据码字。
	capacity := qrDataCapacity(spec)
	bits := &qrBitBuffer{}
	bits.append(0b0100, 4) // 字节模式
	if version >= 10 {
		bits.append(uint32(len(data)), 16)
	} else {
		bits.append(uint32(len(data)), 8)
	}
	for _, b := range data {
		bits.append(uint32(b), 8)
	}
	// 结束符：最多 4 个 0，然后补齐到字节边界。
	remaining := capacity*8 - bits.len()
	if remaining > 4 {
		remaining = 4
	}
	bits.append(0, remaining)
	for bits.len()%8 != 0 {
		bits.append(0, 1)
	}
	// 填充码字：0xEC / 0x11 交替，直到装满容量。
	pads := []uint32{0xEC, 0x11}
	for i := 0; bits.len() < capacity*8; i++ {
		bits.append(pads[i%2], 8)
	}
	dataCodewords := bits.bytes()

	// 3) 分块 + RS 纠错。
	blocks := splitQRBlocks(dataCodewords, spec)
	all := make([]byte, 0, spec.totalCodewords)
	ecBlocks := make([][]byte, len(blocks))
	for i, b := range blocks {
		ecBlocks[i] = qrRSEncode(b, spec.ecPerBlock)
	}
	// 交错：先按列取所有数据块，再按列取所有纠错块。
	maxData := spec.group1Data
	if spec.group2Blocks > 0 {
		maxData = spec.group1Data + 1
	}
	for i := 0; i < maxData; i++ {
		for _, b := range blocks {
			if i < len(b) {
				all = append(all, b[i])
			}
		}
	}
	for i := 0; i < spec.ecPerBlock; i++ {
		for _, b := range ecBlocks {
			if i < len(b) {
				all = append(all, b[i])
			}
		}
	}

	// 4) 铺模块。
	size := 17 + 4*version
	matrix := newQRMatrix(size)
	placeQRFinderPatterns(matrix)
	placeQRAlignPatterns(matrix, version)
	placeQRTimingPatterns(matrix)
	placeQRFormatAreaPlaceholder(matrix)
	placeQRData(matrix, all)

	// 5) 选掩码：8 种都算一遍，取罚分最低的。
	best := [][]bool(nil)
	bestMask := 0
	bestScore := -1
	for mask := 0; mask < 8; mask++ {
		cand := qrApplyMask(matrix, mask)
		placeQRFormatInfo(cand, mask)
		score := qrPenalty(cand)
		if bestScore < 0 || score < bestScore {
			best, bestMask, bestScore = cand, mask, score
		}
	}
	_ = bestMask
	return best, nil
}

// ---------- 位缓冲 ----------

type qrBitBuffer struct {
	buf []byte
	n   int // 已写入的位数
}

func (b *qrBitBuffer) append(value uint32, bits int) {
	for i := bits - 1; i >= 0; i-- {
		bit := (value >> uint(i)) & 1
		byteIdx := b.n / 8
		if byteIdx >= len(b.buf) {
			b.buf = append(b.buf, 0)
		}
		if bit == 1 {
			b.buf[byteIdx] |= 1 << uint(7-b.n%8)
		}
		b.n++
	}
}

func (b *qrBitBuffer) len() int { return b.n }

func (b *qrBitBuffer) bytes() []byte { return b.buf }

// ---------- RS 纠错 ----------

// qrRSEncode 计算 data 的 RS 纠错码字。
//
// 在 GF(256) 上做多项式取模，生成多项式为 (x-a^0)(x-a^1)...(x-a^(ec-1))。
func qrRSEncode(data []byte, ec int) []byte {
	gen := qrRSGenerator(ec)
	// 余数寄存器初始为 0，逐字节推进。
	res := make([]byte, ec)
	for _, b := range data {
		factor := b ^ res[0]
		copy(res, res[1:])
		res[ec-1] = 0
		for i := 0; i < ec; i++ {
			if gen[i] != 0 {
				res[i] ^= qrGFMul(gen[i], factor)
			}
		}
	}
	return res
}

// qrRSGenerator 返回 ec 阶生成多项式的系数（不含最高次）。
func qrRSGenerator(ec int) []byte {
	gen := make([]byte, ec)
	gen[ec-1] = 1
	root := byte(1)
	for i := 0; i < ec; i++ {
		// 乘以 (x - a^i)
		for j := 0; j < ec; j++ {
			gen[j] = qrGFMul(gen[j], root)
			if j+1 < ec {
				gen[j] ^= gen[j+1]
			}
		}
		root = qrGFMul(root, 2)
	}
	return gen
}

// qrGFMul 在 GF(256) 上做乘法，本原多项式 0x11d。
func qrGFMul(a, b byte) byte {
	var result byte
	for b != 0 {
		if b&1 != 0 {
			result ^= a
		}
		hi := a & 0x80
		a <<= 1
		if hi != 0 {
			a ^= 0x1d
		}
		b >>= 1
	}
	return result
}

// splitQRBlocks 按标准把数据码字分块。
func splitQRBlocks(data []byte, spec qrECBlock) [][]byte {
	blocks := make([][]byte, 0, spec.group1Blocks+spec.group2Blocks)
	pos := 0
	for i := 0; i < spec.group1Blocks; i++ {
		blocks = append(blocks, data[pos:pos+spec.group1Data])
		pos += spec.group1Data
	}
	for i := 0; i < spec.group2Blocks; i++ {
		n := spec.group1Data + 1
		blocks = append(blocks, data[pos:pos+n])
		pos += n
	}
	return blocks
}

// ---------- 模块铺设 ----------

func newQRMatrix(size int) [][]bool {
	m := make([][]bool, size)
	for i := range m {
		m[i] = make([]bool, size)
	}
	return m
}

// qrReserved 记录功能图案占用的位置，避免数据写进去。
func qrReserved(size int) [][]bool {
	m := make([][]bool, size)
	for i := range m {
		m[i] = make([]bool, size)
	}
	return m
}

// placeQRFinderPatterns 放置三个定位图案（含分隔符）。
func placeQRFinderPatterns(m [][]bool) {
	size := len(m)
	place := func(row, col int) {
		for dy := -1; dy <= 7; dy++ {
			for dx := -1; dx <= 7; dx++ {
				y, x := row+dy, col+dx
				if y < 0 || y >= size || x < 0 || x >= size {
					continue
				}
				// 7x7 的实心方块 + 中间 3x3 空心。
				inside := (dy >= 0 && dy <= 6 && (dx == 0 || dx == 6)) ||
					(dx >= 0 && dx <= 6 && (dy == 0 || dy == 6)) ||
					(dy >= 2 && dy <= 4 && dx >= 2 && dx <= 4)
				m[y][x] = inside
			}
		}
	}
	place(0, 0)
	place(0, size-7)
	place(size-7, 0)
}

// placeQRAlignPatterns 放置对齐图案。
func placeQRAlignPatterns(m [][]bool, version int) {
	centers := qrAlignCenters[version-1]
	size := len(m)
	for _, cy := range centers {
		for _, cx := range centers {
			// 与定位图案重叠的位置跳过。
			if (cy <= 8 && cx <= 8) || (cy <= 8 && cx >= size-9) || (cy >= size-9 && cx <= 8) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					// 5x5：外圈深色，次圈浅色，中心深色。
					dark := dy == -2 || dy == 2 || dx == -2 || dx == 2 || (dy == 0 && dx == 0)
					m[cy+dy][cx+dx] = dark
				}
			}
		}
	}
}

// placeQRTimingPatterns 放置定时图案（第 6 行与第 6 列交替）。
func placeQRTimingPatterns(m [][]bool) {
	size := len(m)
	for i := 8; i < size-8; i++ {
		m[6][i] = i%2 == 0
		m[i][6] = i%2 == 0
	}
}

// qrFunctionReserved 返回功能图案的占位表。
func qrFunctionReserved(version int) [][]bool {
	size := 17 + 4*version
	res := qrReserved(size)
	mark := func(y, x int) {
		if y >= 0 && y < size && x >= 0 && x < size {
			res[y][x] = true
		}
	}
	// 定位图案 + 分隔符
	for _, p := range [][2]int{{0, 0}, {0, size - 7}, {size - 7, 0}} {
		for dy := -1; dy <= 7; dy++ {
			for dx := -1; dx <= 7; dx++ {
				mark(p[0]+dy, p[1]+dx)
			}
		}
	}
	// 定时图案
	for i := 0; i < size; i++ {
		mark(6, i)
		mark(i, 6)
	}
	// 格式信息
	for i := 0; i <= 8; i++ {
		mark(8, i)
		mark(i, 8)
	}
	for i := 0; i < 8; i++ {
		mark(8, size-1-i)
		mark(size-1-i, 8)
	}
	mark(size-8, 8)
	// 对齐图案
	centers := qrAlignCenters[version-1]
	for _, cy := range centers {
		for _, cx := range centers {
			if (cy <= 8 && cx <= 8) || (cy <= 8 && cx >= size-9) || (cy >= size-9 && cx <= 8) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					mark(cy+dy, cx+dx)
				}
			}
		}
	}
	// 版本信息（v7 起）
	if version >= 7 {
		for i := 0; i < 6; i++ {
			for j := 0; j < 3; j++ {
				mark(size-11+j, i)
				mark(i, size-11+j)
			}
		}
	}
	return res
}

// placeQRFormatAreaPlaceholder 先把格式信息区域占掉（真正的值在选掩码后写）。
//
// 必须**跳过第 6 行与第 6 列**：那是定时图案，不是格式信息。曾经把格式信息的
// 一位写到 (6,8)，把定时图案砸掉了——这个 bug 是被结构测试抓出来的。
func placeQRFormatAreaPlaceholder(m [][]bool) {
	size := len(m)
	for i := 0; i <= 8; i++ {
		if i == 6 {
			continue
		}
		m[8][i] = false
		m[i][8] = false
	}
	for i := 0; i < 8; i++ {
		m[8][size-1-i] = false
		m[size-1-i][8] = false
	}
	m[size-8][8] = true // 固定的深色模块
}

// placeQRData 把码字按标准顺序铺进矩阵。
func placeQRData(m [][]bool, codewords []byte) {
	size := len(m)
	version := (size - 17) / 4
	reserved := qrFunctionReserved(version)

	bitIdx := 0
	total := len(codewords) * 8
	bitAt := func(i int) bool {
		if i >= total {
			return false
		}
		return codewords[i/8]>>uint(7-i%8)&1 == 1
	}

	// 从右下角开始，两列一组向上/向下蛇形推进。
	upward := true
	for x := size - 1; x > 0; x -= 2 {
		if x == 6 { // 跳过定时列
			x = 5
		}
		for i := 0; i < size; i++ {
			y := i
			if upward {
				y = size - 1 - i
			}
			for _, dx := range []int{0, -1} {
				xx := x + dx
				if xx < 0 || reserved[y][xx] {
					continue
				}
				m[y][xx] = bitAt(bitIdx)
				bitIdx++
			}
		}
		upward = !upward
	}
}

// qrApplyMask 复制矩阵并套用一种掩码。
func qrApplyMask(src [][]bool, mask int) [][]bool {
	size := len(src)
	version := (size - 17) / 4
	reserved := qrFunctionReserved(version)

	out := make([][]bool, size)
	for i := range out {
		out[i] = make([]bool, size)
		copy(out[i], src[i])
	}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if reserved[y][x] {
				continue
			}
			if qrMaskBit(mask, y, x) {
				out[y][x] = !out[y][x]
			}
		}
	}
	return out
}

// qrMaskBit 返回某个掩码在 (row, col) 上是否翻转。
func qrMaskBit(mask, row, col int) bool {
	switch mask {
	case 0:
		return (row+col)%2 == 0
	case 1:
		return row%2 == 0
	case 2:
		return col%3 == 0
	case 3:
		return (row+col)%3 == 0
	case 4:
		return (row/2+col/3)%2 == 0
	case 5:
		return (row*col)%2+(row*col)%3 == 0
	case 6:
		return ((row*col)%2+(row*col)%3)%2 == 0
	case 7:
		return ((row+col)%2+(row*col)%3)%2 == 0
	}
	return false
}

// qrFormatBits 返回格式信息的 15 位串（纠错等级 L + 掩码号，含 BCH 校验与掩码）。
//
// 单独抽出来是为了能对标准里写死的值做断言：二维码扫不出来最常见的原因就是
// 这一串算错，值得有独立测试钉住。
func qrFormatBits(mask int) uint32 {
	// 纠错等级 L 的指示位是 01。
	data := uint32(0b01)<<3 | uint32(mask&0b111)
	// BCH(15,5) 校验。
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	return (data<<10 | rem) ^ 0x5412
}

// placeQRFormatInfo 写入格式信息（纠错等级 L + 掩码号）与固定的深色模块。
func placeQRFormatInfo(m [][]bool, mask int) {
	size := len(m)
	bits := qrFormatBits(mask)

	get := func(i int) bool { return bits>>uint(i)&1 == 1 }

	// 左上一组（第 8 列自上而下 + 第 8 行自左而右，跳过定时图案所在的行/列）。
	//
	// 位序容易写错，这里写明每一段的对应关系（i 为格式信息位下标）：
	//   i=0..5  → (8,0)..(8,5)
	//   i=6     → (8,7)          跳过 (8,6)，那是定时列
	//   i=7     → (8,8)
	//   i=8     → (7,8)
	//   i=9..14 → (5,8)..(0,8)   跳过 (6,8)，那是定时行
	for i := 0; i <= 5; i++ {
		m[8][i] = get(i)
	}
	m[8][7] = get(6)
	m[8][8] = get(7)
	m[7][8] = get(8)
	for i := 9; i <= 14; i++ {
		m[14-i][8] = get(i)
	}
	// 另一组：右上（第 8 行右侧）+ 左下（第 8 列下侧）。
	for i := 0; i <= 7; i++ {
		m[size-1-i][8] = get(i)
	}
	for i := 8; i <= 14; i++ {
		m[8][size-15+i] = get(i)
	}
	m[size-8][8] = true
}

// qrPenalty 按标准的四条规则给矩阵打分（越小越好）。
func qrPenalty(m [][]bool) int {
	size := len(m)
	score := 0

	// 规则 1：同色连续 5 个以上。
	for y := 0; y < size; y++ {
		run, prev := 1, m[y][0]
		for x := 1; x < size; x++ {
			if m[y][x] == prev {
				run++
				continue
			}
			if run >= 5 {
				score += 3 + (run - 5)
			}
			run, prev = 1, m[y][x]
		}
		if run >= 5 {
			score += 3 + (run - 5)
		}
	}
	for x := 0; x < size; x++ {
		run, prev := 1, m[0][x]
		for y := 1; y < size; y++ {
			if m[y][x] == prev {
				run++
				continue
			}
			if run >= 5 {
				score += 3 + (run - 5)
			}
			run, prev = 1, m[y][x]
		}
		if run >= 5 {
			score += 3 + (run - 5)
		}
	}

	// 规则 2：2x2 同色块。
	for y := 0; y < size-1; y++ {
		for x := 0; x < size-1; x++ {
			v := m[y][x]
			if m[y][x+1] == v && m[y+1][x] == v && m[y+1][x+1] == v {
				score += 3
			}
		}
	}

	// 规则 3：出现 1:1:3:1:1 的比例且一侧有 4 个以上浅色模块。
	pattern := []bool{true, false, true, true, true, false, true}
	matches := func(get func(int) bool, n int) int {
		count := 0
		for i := 0; i+7 <= n; i++ {
			ok := true
			for j := 0; j < 7; j++ {
				if get(i+j) != pattern[j] {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			// 前四个或后四个是浅色即算命中。
			before := true
			for j := i - 4; j < i; j++ {
				if j < 0 {
					continue
				}
				if get(j) {
					before = false
					break
				}
			}
			after := true
			for j := i + 7; j < i+11; j++ {
				if j >= n {
					continue
				}
				if get(j) {
					after = false
					break
				}
			}
			if before || after {
				count++
			}
		}
		return count
	}
	for y := 0; y < size; y++ {
		score += 40 * matches(func(x int) bool { return m[y][x] }, size)
	}
	for x := 0; x < size; x++ {
		score += 40 * matches(func(y int) bool { return m[y][x] }, size)
	}

	// 规则 4：深色比例偏离 50% 的惩罚。
	dark := 0
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if m[y][x] {
				dark++
			}
		}
	}
	percent := dark * 100 / (size * size)
	deviation := percent - 50
	if deviation < 0 {
		deviation = -deviation
	}
	score += (deviation / 5) * 10

	return score
}
