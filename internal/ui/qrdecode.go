package ui

import (
	"fmt"
)

// 二维码解码器——**只用于测试**，但它极其重要。
//
// 为什么要有它：用户实机扫码时「连识别都识别不出来是二维码」，而我自己写的
// 编码器靠「结构看着对」是验不出这类问题的。所以这里实现一个完整的解码器，
// 让「编码 → 矩阵 → 解码 → 原文」形成闭环：只要闭环成立，矩阵在数学上就是
// 一个合法二维码，剩下的只是终端渲染与手机摄像头的物理问题。
//
// 解码步骤与编码对称：
//  1. 从矩阵读回格式信息，取出掩码号并校验 BCH。
//  2. 去掉掩码。
//  3. 按标准顺序读回码字。
//  4. 分块去交错。
//  5. 读模式与长度，取出原文。

// qrDecode 从矩阵里解出原文。
func qrDecode(matrix [][]bool) ([]byte, error) {
	size := len(matrix)
	if size < 21 || (size-17)%4 != 0 {
		return nil, fmt.Errorf("尺寸 %d 不是一个合法二维码边长", size)
	}
	version := (size - 17) / 4
	if version < 1 || version > len(qrVersionsL) {
		return nil, fmt.Errorf("不支持的版本 %d", version)
	}

	// 1) 读格式信息（左上那一份），并校验 BCH。
	mask, ecLevel, err := readQRFormatInfo(matrix)
	if err != nil {
		return nil, err
	}
	if ecLevel != 0b01 {
		return nil, fmt.Errorf("本解码器只支持纠错等级 L，实际读到 %02b", ecLevel)
	}

	// 2) 去掉掩码。
	unmasked := qrApplyMask(matrix, mask)

	// 3) 按标准顺序读回码字。
	spec := qrVersionsL[version-1]
	raw, err := readQRCodewords(unmasked, version, spec.totalCodewords)
	if err != nil {
		return nil, err
	}

	// 4) 分块去交错，取出数据码字。
	data, err := deinterleaveQR(raw, spec)
	if err != nil {
		return nil, err
	}

	// 5) 读模式与长度。
	bits := &qrBitReader{data: data}
	mode, err := bits.read(4)
	if err != nil {
		return nil, err
	}
	if mode != 0b0100 {
		return nil, fmt.Errorf("本解码器只支持字节模式，实际模式 %04b", mode)
	}
	lenBits := 8
	if version >= 10 {
		lenBits = 16
	}
	n, err := bits.read(lenBits)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, n)
	for i := 0; i < int(n); i++ {
		b, err := bits.read(8)
		if err != nil {
			return nil, err
		}
		out = append(out, byte(b))
	}
	return out, nil
}

// readQRFormatInfo 读回左上那份格式信息，校验 BCH 并返回掩码号与纠错等级。
func readQRFormatInfo(m [][]bool) (mask int, ecLevel int, err error) {
	// 位序与 placeQRFormatInfo 对称。
	var bits uint32
	pick := func(pos int, v bool) {
		if v {
			bits |= 1 << uint(pos)
		}
	}
	for i := 0; i <= 5; i++ {
		pick(i, m[8][i])
	}
	pick(6, m[8][7])
	pick(7, m[8][8])
	pick(8, m[7][8])
	for i := 9; i <= 14; i++ {
		pick(i, m[14-i][8])
	}

	// 去掉固定掩码后校验 BCH。
	val := bits ^ 0x5412
	// 高 5 位是数据，低 10 位是校验。
	data := val >> 10
	check := val & 0x3ff
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	if rem != check {
		return 0, 0, fmt.Errorf("格式信息的 BCH 校验失败（读到 %015b）", bits)
	}
	ecLevel = int(data >> 3 & 0b11)
	mask = int(data & 0b111)
	return mask, ecLevel, nil
}

// readQRCodewords 按标准顺序从矩阵里读回码字。
//
// 顺序必须与 placeQRData 完全对称：从右下角开始，两列一组蛇形向上/向下，
// 每组先右列后左列，跳过功能图案与第 6 列。
func readQRCodewords(m [][]bool, version, total int) ([]byte, error) {
	size := len(m)
	reserved := qrFunctionReserved(version)

	out := make([]byte, 0, total)
	var cur byte
	bitIdx := 0
	push := func(v bool) {
		if bitIdx%8 == 0 {
			cur = 0
		}
		if v {
			cur |= 1 << uint(7-bitIdx%8)
		}
		bitIdx++
		if bitIdx%8 == 0 {
			out = append(out, cur)
		}
	}

	upward := true
	for x := size - 1; x > 0; x -= 2 {
		if x == 6 {
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
				push(m[y][xx])
			}
		}
		upward = !upward
	}
	if len(out) < total {
		return nil, fmt.Errorf("只读到 %d 个码字，应有 %d 个", len(out), total)
	}
	return out[:total], nil
}

// deinterleaveQR 按标准把交错码字拆回数据块，再拼回数据码字序列。
func deinterleaveQR(raw []byte, spec qrECBlock) ([]byte, error) {
	nBlocks := spec.group1Blocks + spec.group2Blocks
	if nBlocks == 0 {
		return nil, fmt.Errorf("分块参数非法")
	}
	blocks := make([][]byte, nBlocks)
	for i := range blocks {
		n := spec.group1Data
		if i >= spec.group1Blocks {
			n = spec.group1Data + 1
		}
		blocks[i] = make([]byte, 0, n)
	}

	// 与 splitQRBlocks 的交错顺序对称：先按列取数据码字。
	pos := 0
	maxData := spec.group1Data
	if spec.group2Blocks > 0 {
		maxData = spec.group1Data + 1
	}
	for i := 0; i < maxData; i++ {
		for b := range blocks {
			capacity := spec.group1Data
			if b >= spec.group1Blocks {
				capacity = spec.group1Data + 1
			}
			if i >= capacity {
				continue
			}
			if pos >= len(raw) {
				return nil, fmt.Errorf("码字不足，去交错中断")
			}
			blocks[b] = append(blocks[b], raw[pos])
			pos++
		}
	}
	total := 0
	for _, b := range blocks {
		total += len(b)
	}
	if total != qrDataCapacity(spec) {
		return nil, fmt.Errorf("去交错得到 %d 个数据码字，应有 %d 个", total, qrDataCapacity(spec))
	}

	out := make([]byte, 0, total)
	for _, b := range blocks {
		out = append(out, b...)
	}
	return out, nil
}

// qrBitReader 按位读取字节序列。
type qrBitReader struct {
	data []byte
	pos  int
}

func (r *qrBitReader) read(n int) (uint32, error) {
	var out uint32
	for i := 0; i < n; i++ {
		byteIdx := r.pos / 8
		if byteIdx >= len(r.data) {
			return 0, fmt.Errorf("数据不够读（需要 %d 位，已读 %d 位）", n, r.pos)
		}
		bit := r.data[byteIdx] >> uint(7-r.pos%8) & 1
		out = out<<1 | uint32(bit)
		r.pos++
	}
	return out, nil
}
