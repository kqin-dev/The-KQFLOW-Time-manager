package ui

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kqin-dev/The-KQFLOW-Time-manager/internal/model"
)

// ntfy.sh 推送（见需求 3 的第三档：人离开设备、只带了手机）。
//
// 行为按用户要求定：**纯单向推送，失败就算了**。不重试、不排队：计时体验
// 不该被网络左右，一次没收到也比卡住界面强。
//
// 频道名由程序生成高熵随机串而不是让用户自己起（见 config.GenerateNtfyTopic）：
// ntfy 频道默认全网公开，靠用户的安全意识去避免被猜到是不现实的。

// NtfyTimeout 是单次推送的超时。
//
// 短一点：这是后台尽力而为的通知，卡住很久既没用又占资源。
const NtfyTimeout = 4 * time.Second

// NtfyMaxBody 是推送正文长度上限（ntfy 自身也有上限，这里提前截断）。
const NtfyMaxBody = 512

// ntfyPushedMsg 报告推送结果；失败只提示一次，不影响计时。
type ntfyPushedMsg struct {
	err error
}

// ntfyPushCmd 返回把「进入某个时段」推送到手机的命令。
//
// 未启用时返回 nil，调用方拿到的就是「什么都不做」。
func (a *App) ntfyPushCmd(seg model.Segment) tea.Cmd {
	if a.cfg == nil || !a.cfg.NtfyReady() {
		return nil
	}
	title, body := a.notifyText(seg)
	server := a.cfg.NtfyServerURL()
	topic := strings.TrimSpace(a.cfg.NtfyTopic)

	return func() tea.Msg {
		return ntfyPushedMsg{err: pushNtfy(server, topic, title, body)}
	}
}

// pushNtfy 把一条消息推送到 ntfy 频道。
//
// ntfy 的发布方式很直接：POST 到 `<server>/<topic>`，正文就是消息内容，
// 标题放在 X-Title 头里。不需要账号（这也是需求选它的原因：免登录）。
func pushNtfy(server, topic, title, body string) error {
	server = strings.TrimRight(strings.TrimSpace(server), "/")
	topic = strings.TrimSpace(topic)
	if server == "" || topic == "" {
		return fmt.Errorf("ntfy 服务地址或频道名为空")
	}
	if _, err := url.Parse(server); err != nil {
		return fmt.Errorf("ntfy 服务地址不合法: %w", err)
	}
	if len(body) > NtfyMaxBody {
		body = body[:NtfyMaxBody]
	}

	endpoint := server + "/" + url.PathEscape(topic)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewBufferString(body))
	if err != nil {
		return fmt.Errorf("构造推送请求失败: %w", err)
	}
	if title != "" {
		// ntfy 对非 ASCII 标题的支持依赖这个头，中文标题必须能正常显示。
		req.Header.Set("X-Title", title)
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")

	client := &http.Client{Timeout: NtfyTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("推送失败（网络不可达或地址有误）: %w", err)
	}
	defer resp.Body.Close()
	// 读掉一小段响应体，便于复用连接，也让错误信息更有用。
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(snippet))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("推送被拒绝：%s", msg)
	}
	return nil
}
