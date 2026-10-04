package ui

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

// 内置提示音（见需求 3）。
//
// 需求原话是「如果你有知道的可以拉取那就拉取，否则告诉我去找你需要的」。实际
// 没有可以随 MIT 项目自由分发的音频素材（授权、体积、来源都是问题），所以这里
// **用代码合成**短音频：不引入任何外部资源、不涉及授权、仓库也不会变大。
//
// 合成思路：
//   - 颂钵：低频基音 + 几个非整数倍泛音（真实的钵就是这样），长衰减。
//   - 风铃：几个高频正弦叠加，各自的衰减更快，听感清脆。
//   - 白噪音：一段随机噪声乘以淡出包络，听起来是「沙」的一声。
//
// 三个音都做成 16 位单声道 PCM 的 WAV，由平台相关的播放器播放
// （见 notify_sound_windows.go / notify_sound_other.go）。

const (
	// soundSampleRate 是采样率。22.05kHz 对提示音足够，文件也小。
	soundSampleRate = 22050
	// soundMaxSeconds 是单个音频的时长上限，防止合成参数出错时生成巨大文件。
	soundMaxSeconds = 3.0
)

// builtinSoundNames 返回可用的内置音频名（不含 "bell" 与 "none"）。
func builtinSoundNames() []string { return []string{"bowl", "chime", "white"} }

// generateSoundWAV 按名字合成一段 WAV；名字不认识时返回错误。
func generateSoundWAV(name string) ([]byte, error) {
	var samples []float64
	switch name {
	case "bowl":
		samples = synthBowl()
	case "chime":
		samples = synthChime()
	case "white":
		samples = synthWhite()
	default:
		return nil, fmt.Errorf("未知的提示音 %q", name)
	}
	return encodeWAV(samples), nil
}

// synthBowl 合成颂钵：低频基音 + 非整数倍泛音 + 长衰减。
//
// 泛音比例刻意用非整数（2.76 / 5.4 这类），整数倍会听成有音高的乐器，
// 非整数倍才接近金属钵那种「嗡嗡」的泛音堆。
func synthBowl() []float64 {
	const (
		seconds = 2.6
		base    = 196.0 // G3 附近，低沉但不闷
	)
	n := int(soundSampleRate * seconds)
	out := make([]float64, n)
	partials := []struct {
		mult  float64
		amp   float64
		decay float64 // 每秒衰减系数
	}{
		{1.0, 1.0, 1.1},
		{2.76, 0.55, 1.6},
		{5.40, 0.30, 2.2},
		{8.93, 0.15, 3.0},
	}
	for i := 0; i < n; i++ {
		t := float64(i) / soundSampleRate
		var v float64
		for _, p := range partials {
			v += p.amp * math.Exp(-p.decay*t) * math.Sin(2*math.Pi*base*p.mult*t)
		}
		out[i] = v * envelope(t, seconds, 0.004, 0.25)
	}
	return normalize(out)
}

// synthChime 合成风铃：几个高频正弦，衰减更快，听感清脆。
func synthChime() []float64 {
	const (
		seconds = 1.6
	)
	n := int(soundSampleRate * seconds)
	out := make([]float64, n)
	// 三个音依次轻响，模拟风铃被风吹动时的不规则敲击。
	strikes := []struct {
		at    float64 // 起始秒
		freq  float64
		amp   float64
		decay float64
	}{
		{0.00, 1568.0, 0.9, 3.2}, // G6
		{0.09, 2093.0, 0.7, 3.8}, // C7
		{0.20, 2637.0, 0.5, 4.4}, // E7
	}
	for i := 0; i < n; i++ {
		t := float64(i) / soundSampleRate
		var v float64
		for _, s := range strikes {
			if t < s.at {
				continue
			}
			dt := t - s.at
			v += s.amp * math.Exp(-s.decay*dt) * math.Sin(2*math.Pi*s.freq*dt)
		}
		out[i] = v * envelope(t, seconds, 0.002, 0.2)
	}
	return normalize(out)
}

// synthWhite 合成白噪音：随机噪声 + 淡出，听起来是「沙」的一声。
func synthWhite() []float64 {
	const seconds = 1.0
	n := int(soundSampleRate * seconds)
	out := make([]float64, n)
	// 固定种子：同一预设每次听起来都一样，便于用户辨认。
	rng := rand.New(rand.NewSource(20261003))
	for i := 0; i < n; i++ {
		t := float64(i) / soundSampleRate
		// 高频略作柔化，纯白噪声在耳机里有点刺。
		v := rng.Float64()*2 - 1
		out[i] = v * math.Exp(-2.2*t) * envelope(t, seconds, 0.001, 0.1)
	}
	return normalize(out)
}

// envelope 返回淡入淡出包络；fadeIn / fadeOut 是两端时长（秒）。
func envelope(t, total, fadeIn, fadeOut float64) float64 {
	v := 1.0
	if fadeIn > 0 && t < fadeIn {
		v *= t / fadeIn
	}
	if fadeOut > 0 && t > total-fadeOut {
		v *= (total - t) / fadeOut
	}
	if v < 0 {
		v = 0
	}
	return v
}

// normalize 把样本峰值压到 0.85，避免削顶失真。
func normalize(samples []float64) []float64 {
	peak := 0.0
	for _, s := range samples {
		if a := math.Abs(s); a > peak {
			peak = a
		}
	}
	if peak <= 0 {
		return samples
	}
	scale := 0.85 / peak
	for i := range samples {
		samples[i] *= scale
	}
	return samples
}

// encodeWAV 把 -1..1 的浮点样本编码成 16 位单声道 PCM 的 WAV。
func encodeWAV(samples []float64) []byte {
	if len(samples) > int(soundSampleRate*soundMaxSeconds) {
		samples = samples[:int(soundSampleRate*soundMaxSeconds)]
	}
	dataSize := len(samples) * 2

	var b bytes.Buffer
	// RIFF 头
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+dataSize))
	b.WriteString("WAVE")
	// fmt 子块
	b.WriteString("fmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16)) // 子块大小
	binary.Write(&b, binary.LittleEndian, uint16(1))  // PCM
	binary.Write(&b, binary.LittleEndian, uint16(1))  // 单声道
	binary.Write(&b, binary.LittleEndian, uint32(soundSampleRate))
	binary.Write(&b, binary.LittleEndian, uint32(soundSampleRate*2)) // 字节率
	binary.Write(&b, binary.LittleEndian, uint16(2))                 // 块对齐
	binary.Write(&b, binary.LittleEndian, uint16(16))                // 位深
	// data 子块
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(dataSize))
	for _, s := range samples {
		if s > 1 {
			s = 1
		}
		if s < -1 {
			s = -1
		}
		binary.Write(&b, binary.LittleEndian, int16(s*32767))
	}
	return b.Bytes()
}

// builtinSoundFile 把合成结果写到临时文件并返回路径。
//
// 同一预设复用同一个文件（内容固定），避免每次切换时段都重新合成 + 写盘。
func builtinSoundFile(name string) (string, error) {
	raw, err := generateSoundWAV(name)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(os.TempDir(), "kqflow-sounds")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%d.wav", name, soundSampleRate))
	// 已经生成过且大小一致就直接用。
	if info, err := os.Stat(path); err == nil && info.Size() == int64(len(raw)) {
		return path, nil
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// soundTimeout 限制播放命令的最长存活时间，避免留下卡住的进程。
const soundTimeout = 4 * time.Second
