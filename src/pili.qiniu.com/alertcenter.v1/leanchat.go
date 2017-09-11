package alertcenter

import (
	"errors"
	"fmt"
	"time"

	"github.com/qiniu/rpc.v1/lb.v2.1"
	"github.com/qiniu/xlog.v1"
)

const (
	DefaultLeanChatTimeLayout          = "2006-01-02 15:04:05.000000"
	DefaultLeanChatTimeHeader          = "[StartsAt]"
	DefaultLeanAlertIdHeader           = "[AlertId]"
	DefaultLeanChatPath                = "/services"
	DefaultLeanChatDisplayUser         = "Cronus"
	DefaultLeanChatMoreAlertsText      = "更多告警请点我"
	DefaultLeanChatMaxDisplayCnt       = 3
	DefaultLeanChatMinCloseAlertsTimes = 3
)

type LeanChatCfg struct {
	Hosts           []string `json:"hosts"`
	ServiceId       string   `json:"service_id"`
	Path            string   `json:"path"`
	Channel         string   `json:"channel"`
	PhotoUrl        string   `json:"photo_url"`
	DisplayUserName string   `json:"name"`
	// 头像Url
	AvatarUrl string `json:"avataurl"`

	TimeLayout string `json:"time_layout"`

	// 告警消息里AlertId的头部样式
	AlertIdHeader string `json:"alertid_header"`
	// 告警消息里时间的头部样式
	TimeHeader string `json:"time_header"`
	// 表示零信里一条消息里最多可以装多少条告警
	MaxDisplayCnt int `json:"max_display_count"`

	// 表示告警条数超过MaxDisplayCnt后，按钮显示的文本内容
	MoreAlertsText string `json:"more_alerts_text"`

	// 表示重新发送告警后多少次后出现可以关闭该告警的按钮
	MinCloseAlertsTimes int `json:"min_close_display_times"`

	DialTimeoutMs int    `json:"dial_timeout_ms"`
	TryTimes      uint32 `json:"try_times"`
}

type LeanChat struct {
	*LeanChatCfg
	From string
	cli  *lb.Client
}

func CheckLeanChatCfg(cfg *LeanChatCfg) {
	if cfg.TimeLayout == "" {
		cfg.TimeLayout = DefaultLeanChatTimeLayout
	}
	if cfg.AlertIdHeader == "" {
		cfg.AlertIdHeader = DefaultLeanAlertIdHeader
	}
	if cfg.TimeHeader == "" {
		cfg.TimeHeader = DefaultLeanChatTimeHeader
	}
	if cfg.Path == "" {
		cfg.Path = DefaultLeanChatPath
	}
	if cfg.DisplayUserName == "" {
		cfg.DisplayUserName = DefaultLeanChatDisplayUser
	}
	if cfg.MoreAlertsText == "" {
		cfg.MoreAlertsText = DefaultLeanChatMoreAlertsText
	}

	if cfg.MaxDisplayCnt == 0 {
		cfg.MaxDisplayCnt = DefaultLeanChatMaxDisplayCnt
	}

	if cfg.MinCloseAlertsTimes == 0 {
		cfg.MinCloseAlertsTimes = DefaultLeanChatMinCloseAlertsTimes
	}
}

func NewLeanChat(cfg *LeanChatCfg) *LeanChat {
	CheckLeanChatCfg(cfg)

	transport := lb.NewTransport(&lb.TransportConfig{
		DialTimeoutMS: cfg.DialTimeoutMs,
	})

	return &LeanChat{
		LeanChatCfg: cfg,
		cli: lb.New(
			&lb.Config{
				Hosts:    cfg.Hosts,
				TryTimes: cfg.TryTimes,
			}, transport),
	}
}

type leanChatColor string

const (
	LeanChatWarning leanChatColor = "warning"
	LeanChatInfo    leanChatColor = "info"
	LeanChatPrimary leanChatColor = "primary"
	LeanChatError   leanChatColor = "error"
	LeanChatMuted   leanChatColor = "muted"
	LeanChatSuccess leanChatColor = "success"
)

func NewLeanChatColor(s Severity) (color leanChatColor) {
	switch s {
	case SeverityInfo:
		color = LeanChatInfo
	case SeverityWarning:
		color = LeanChatWarning
	case SeverityCritical:
		color = LeanChatError
	case SeveritySuccess:
		color = LeanChatSuccess
	default:
		color = LeanChatInfo
	}
	return
}

type leanChatAttachment struct {
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Url         string        `json:"url"`
	Color       leanChatColor `json:"color"`
}

type leanChatDisplayUser struct {
	Name      string `json:"name"`
	AvatarUrl string `json:"avatarUrl"`
}

type leanChatButton struct {
	Text        string `json:"text"`
	Url         string `json:"url"`
	Action      string `json:"action"`
	CallbackUrl string `json:"callbackUrl"`
}

type leanChatReq struct {
	Text        string               `json:"text"`
	Channel     string               `json:"channel"`
	PhotoUrl    string               `json:"photoUrl"`
	Attachments []leanChatAttachment `json:"attachments"`
	DisplayUser leanChatDisplayUser  `json:"displayUser"`
	Buttons     []leanChatButton     `json:"buttons"`
}

type leanChatResp struct {
	Error int `json:"error"`
	Data  struct {
		Message   string `json:"message"`
		RequestId string `json:"requestId"`
	} `json:"data"`
}

func (n *LeanChat) Name() string {
	return "LeanChat"
}

func (n *LeanChat) Notify(xl *xlog.Logger, msg *Message) (err error) {
	if len(msg.Alerts) == 0 {
		return
	}
	atts := make([]leanChatAttachment, 0, len(msg.Alerts))
	descs := ""

	for i, a := range msg.Alerts {
		att := leanChatAttachment{
			Title:       n.GetTitle(a),       // Title:       "[PILI] vdn-gzgy-tel-1-2 pili-streamd fd 3013 > 3000 | firing | 第 1 次",
			Description: n.GetDescription(a), // Description: "[StartsAt] 2016-11-23 21:58:37",
			Url:         a.GeneratorURL,
			Color:       NewLeanChatColor(a.Severity),
		}
		descs += a.Description + " | "
		atts = append(atts, att)

		if i == n.MaxDisplayCnt-1 {
			break
		}
	}

	req := &leanChatReq{
		Text:        n.GetText2(descs),
		PhotoUrl:    n.PhotoUrl,
		Attachments: atts,
	}

	if len(msg.Alerts) > n.MaxDisplayCnt {
		req.Buttons = []leanChatButton{
			{
				Text: n.GetMoreAlertsText(len(msg.Alerts)),
			},
		}
	}

	if msg.Alerts[0].IsEmergent {
		req.Buttons = []leanChatButton{
			{
				Text: n.GetEmergencyText(),
			},
		}
	}

	err = n.SendMsg(xl, req)
	return
}

func (n *LeanChat) SendMsg(xl *xlog.Logger, req *leanChatReq) (err error) {

	if req.Channel == "" {
		req.Channel = n.Channel
	}
	if req.DisplayUser.AvatarUrl == "" {
		req.DisplayUser.AvatarUrl = n.AvatarUrl
	}
	if req.DisplayUser.Name == "" {
		req.DisplayUser.Name = n.DisplayUserName
	}

	var res leanChatResp
	err = n.cli.CallWithJson(xl, &res, n.GetPath(), req)
	if err != nil {
		xl.Errorf("LeanChat Notify(msg) error: %+v", err)
		return
	}
	if res.Error != 0 {
		xl.Errorf("Req: %+v, Resp: {Error: %+v, Messsage: %+v}", req, res.Error, res.Data.Message)
		err = errors.New(fmt.Sprintf("%v %v", res.Error, res.Data.Message))
	}
	return
}

func (n *LeanChat) GetText(from string) string {
	return fmt.Sprintf("[From] %v           [Time] %v", from, time.Now().Format(n.TimeLayout))
}

func (n *LeanChat) GetText2(descs string) string {
	return fmt.Sprintf("%v [Time] %v", descs, time.Now().Format(n.TimeLayout))
}

func (n *LeanChat) GetPath() string {
	return fmt.Sprintf("%v/%v", n.Path, n.ServiceId)
}

func (n *LeanChat) GetMoreAlertsText(cnt int) string {
	return fmt.Sprintf("共有 %v 个告警 %v", cnt, n.MoreAlertsText)
}

func (n *LeanChat) GetTitle(a *Alert) string {
	if a.Status == AlertResolved {
		return fmt.Sprintf("%v | %v ", a.Description, a.Status)
	}
	return fmt.Sprintf("%v | %v", a.Description, a.Status)
}

func (n *LeanChat) GetDescription(a *Alert) string {
	return fmt.Sprintf("%v %v    %s %s", n.TimeHeader, a.StartsAt.Format(n.TimeLayout), DefaultLeanAlertIdHeader, a.Id)
}

func (n *LeanChat) GetEmergencyText() string {
	return fmt.Sprintf("该告警被升级请赶紧处理告警")
}
