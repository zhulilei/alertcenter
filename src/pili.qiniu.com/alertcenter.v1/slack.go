package alertcenter

import (
	"fmt"
	"strings"

	"github.com/qiniu/log.v1"
	"github.com/qiniu/rpc.v1/lb.v2.1"
	"github.com/qiniu/xlog.v1"
)

const (
	DefaultSlackTimeLayout          = "2006-01-02 15:04:05.000000"
	DefaultSlackTimeHeader          = "[StartsAt]"
	DefaultSlackAlertIdHeader       = "[AlertId]"
	DefaultSlackAlertKeyHeader      = "[Key]"
	DefaultSlackPath                = "/services"
	DefaultSlackUsername            = "Cronus"
	DefaultSlackMoreAlertsText      = "更多告警请点我"
	DefaultSlackMaxDisplayCnt       = 3
	DefaultSlackMinCloseAlertsTimes = 3
)

type SlackCfg struct {
	Name      string   `json:"name"`
	Hosts     []string `json:"hosts"`
	ServiceId string   `json:"service_id"`
	Path      string   `json:"path"`
	// Channel   string   `json:"channel"`
	// 头像Url
	IconUrl    string `json:"icon_url"`
	IconEmoji  string `json:"icon_emoji"`
	Username   string `json:"username"`
	FooterIcon string `json:"footer_icon"`

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

	PortalUrl string `json:"portal_url"`
}

type Slack struct {
	*SlackCfg
	cli *lb.Client
}

func (cfg *SlackCfg) Check() {
	if cfg.Name == "" {
		log.Panic("miss Name of slackCfg")
	}
	if cfg.TimeLayout == "" {
		cfg.TimeLayout = DefaultSlackTimeLayout
	}
	if cfg.AlertIdHeader == "" {
		cfg.AlertIdHeader = DefaultLeanAlertIdHeader
	}
	if cfg.TimeHeader == "" {
		cfg.TimeHeader = DefaultSlackTimeHeader
	}
	if cfg.Path == "" {
		cfg.Path = DefaultSlackPath
	}
	if cfg.Username == "" {
		cfg.Username = DefaultSlackUsername
	}
	if cfg.MoreAlertsText == "" {
		cfg.MoreAlertsText = DefaultSlackMoreAlertsText
	}

	if cfg.MaxDisplayCnt == 0 {
		cfg.MaxDisplayCnt = DefaultSlackMaxDisplayCnt
	}

	if cfg.MinCloseAlertsTimes == 0 {
		cfg.MinCloseAlertsTimes = DefaultSlackMinCloseAlertsTimes
	}
}

func NewSlack(cfg SlackCfg) *Slack {
	cfg.Check()

	return &Slack{
		SlackCfg: &cfg,
		cli: lb.New(
			&lb.Config{
				Hosts:    cfg.Hosts,
				TryTimes: cfg.TryTimes,
			}, nil),
	}
}

type SlackColor string

const (
	SlackGood    SlackColor = "good"
	SlackWarning SlackColor = "warning"
	SlackDanger  SlackColor = "danger"
)

func NewSlackColor(s Severity) (color SlackColor) {
	switch s {
	case SeverityWarning:
		color = SlackWarning
	case SeverityCritical:
		color = SlackDanger
	case SeveritySuccess:
		color = SlackGood
	default:
		color = SlackWarning
	}
	return
}

type SlackAttachment struct {
	Fallback   string     `json:"fallback,omitempty"`
	Text       string     `json:"text,omitempty"`
	Title      string     `json:"title,omitempty"`
	TitleLink  string     `json:"title_link,omitempty"`
	ImageUrl   string     `json:"image_url,omitempty"`
	Footer     string     `json:"footer,omitempty"`
	FooterIcon string     `json:"footer_icon,omitempty"`
	Ts         string     `json:"ts,omitempty"`
	Color      SlackColor `json:"color,omitempty"`
	MrkdwnIn   []string   `json:"mrkdwn_in,omitempty"`
}

type SlackReq struct {
	Text        string            `json:"text,omitempty"`
	Username    string            `json:"username,omitempty"`
	IconUrl     string            `json:"icon_url,omitempty"`
	IconEmoji   string            `json:"icon_emoji,omitempty"`
	Attachments []SlackAttachment `json:"attachments,omitempty"`
}

type SlackResp struct {
}

func (n *Slack) Name() string {
	return n.SlackCfg.Name
}

func (n *Slack) Notify(msg Message) (err error) {
	xl := msg.xl
	if len(msg.Alerts) == 0 {
		return
	}
	atts := make([]SlackAttachment, 0, len(msg.Alerts))

	for i, a := range msg.Alerts {
		if !strings.HasPrefix(a.GeneratorURL, "http://") && !strings.HasPrefix(a.GeneratorURL, "https://") {
			a.GeneratorURL = "http://" + a.GeneratorURL
		}
		att := SlackAttachment{
			Fallback:   n.GetTitle(a),
			Title:      n.GetTitle(a), // [PILI] vdn-gzgy-tel-1-2 pili-streamd fd 3013 > 3000 | firing | 第 1 次
			TitleLink:  a.GeneratorURL,
			Footer:     n.GetFooter(a),
			FooterIcon: n.FooterIcon,
			Ts:         n.GetTs(a), // 2016-11-23 21:58:37",
			Color:      NewSlackColor(a.Severity),
			MrkdwnIn:   []string{"text"},
		}
		if len(a.AnalyzerTypes) != 0 {
			att.Text = n.GetAnalyzerResults(a)
		}
		atts = append(atts, att)

		if i == n.MaxDisplayCnt-1 {
			break
		}
	}

	req := &SlackReq{
		IconUrl:     n.IconUrl,
		IconEmoji:   n.IconEmoji,
		Attachments: atts,
	}

	if len(msg.Alerts) > n.MaxDisplayCnt {
		req.Attachments = append(req.Attachments, SlackAttachment{
			Title:     n.GetMoreAlertsText(len(msg.Alerts)),
			TitleLink: n.PortalUrl,
		})
	}

	if msg.Alerts[0].IsEmergent {
		req.Attachments = append(req.Attachments, SlackAttachment{
			Title:     n.GetEmergencyText(),
			TitleLink: n.PortalUrl,
		})
	}

	err = n.SendMsg(xl, req)
	return
}

func (n *Slack) SendMsg(xl *xlog.Logger, req *SlackReq) (err error) {
	if req.IconUrl == "" {
		req.IconUrl = n.IconUrl
	}
	if req.Username == "" {
		req.Username = n.Username
	}

	// var res SlackResp
	err = n.cli.CallWithJson(xl, nil, n.GetPath(), req)
	if err != nil {
		xl.Errorf("Slack Notify(msg) error: %+v", err)
		return
	}
	return
}

func (n *Slack) GetPath() string {
	return fmt.Sprintf("%v/%v", n.Path, n.ServiceId)
}

func (n *Slack) GetMoreAlertsText(cnt int) string {
	return fmt.Sprintf("共有 %v 个告警 %v", cnt, n.MoreAlertsText)
}

func (n *Slack) GetTitle(a *Alert) string {
	if a.Status == AlertResolved && a.IsEmergent {
		return fmt.Sprintf("%v | %v | 该告警是升级的告警", a.Description, a.Status)
	}
	return fmt.Sprintf("%v | %v", a.Description, a.Status)
}

func (n *Slack) GetFooter(a *Alert) string {
	s := fmt.Sprintf("%s %s %s %s", DefaultLeanAlertIdHeader, a.Id.Hex(), DefaultSlackAlertKeyHeader, a.Key)
	if a.IsEmergent {
		s += fmt.Sprintf(" <%s|[%s]>", n.PortalUrl, n.GetEmergencyText())
	}
	return s
}

func (n *Slack) GetTs(a *Alert) string {
	return fmt.Sprintf("%v", a.StartsAt.Unix())
}

func (n *Slack) GetEmergencyText() string {
	return fmt.Sprintf("该告警被升级请赶紧处理告警")
}

func (n *Slack) GetAnalyzerResults(a *Alert) (text string) {
	for i, t := range a.AnalyzerTypes {
		text += fmt.Sprintf("*<%v/loganalyzer?type=%v&alertId=%v|点击查看 %v 类型告警分析结果>*", n.PortalUrl, t, a.Id.Hex(), t)
		if i != len(a.AnalyzerTypes)-1 {
			text += "\n"
		}
	}
	return
}
