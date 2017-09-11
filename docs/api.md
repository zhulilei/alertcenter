# 告警中心

## API

### 1 基本 API
#### 1.1 新增告警
请求包
```
POST /alerts
Host: pili-bc-alertcenter.qiniuapi.com
Authorization: <QiniuAdminToken>
{
  "alerts": [
    {
      "alertname":    "<alertname>",  // 告警名称，例如：pili_vdn_node_uptime
      "desc":         "<desc>",       // 告警的主要信息，比如：域名解析未同步
      "status":       "<status>",     // 触发状态(选填)，不填则是 firing ，可选值：firing|resolved，
      "severity":     "<severity>",   // 告警级别，可选值：warning | critical
      "startsAt":     "<startsAt>",   // 告警发生时间，2016-11-24T00:21:45.887+08:00
      "generatorURL": "<url>",        // 可选，title 的超链接如果有的话，必须带有http或者https的开头
      "labels": {                     // 附加信息(选填)，KV 形式
          "<key>":    "<value>"  
      }
    },
    ...
  ],
  "source": "<source>"              // 可选，告警来自什么服务，比如：pili-zeus，将来作为 tags 里一个 tag
}
```

返回包
```
200 {}
400 {
    "error": "invalid ID"
}
400 {
    "error": "invalid comparison"
}
409 {
    "error": "duplicated alert"
}
```


#### 1.2 获取当前告警
请求包
```
GET /alerts/active
Host: pili-bc-alertcenter.qiniuapi.com
Authorization: <QiniuAdminToken>
```

返回包
```
200 {
  "alerts": [
    {
      "id":           "<id>",         // 告警 ID
      "key":          "<key>",        // 告警的 key
      "alertname":    "<alertname>",  // 告警名称，例如：pili_vdn_node_uptime
      "isSilence":    "<isSilence>",  // 是否被 silence
      "desc":         "<desc>",       // 告警的主要信息，比如：域名解析未同步
      "isEmergent":   "<isEmergent>", // 是否告警升级了
      "status":       "<status>",     // 触发状态(选填)，不填则是 firing ，可选值：firing|resolved，
      "severity":     "<severity>",   // 告警级别，可选值：P1(warning) | P0(critical)
      "startsAt":     "<startsAt>",   // 告警发生时间，2016-11-24T00:21:45.887+08:00
      "generatorURL": "<url>",        // 可选，title 的超链接如果有的话，必须带有 http 或者 https 的开头
      "labels": {                     // 附加信息(选填)，KV 形式
        "<key>":      "<value>"  
      }
    },
    ...
  ],
  "tags": {
    "alertname1": [ "<tag1>", "<tag2>"],
    ...
  }
}
```

#### 1.3 获取告警历史
请求包
```
GET /alerts/history?&alertname=<alertname>&key=<key>&tags=[<tags>]&begin=<begin>&end=<end>&limit=<limit>&marker=<marker>
Host: pili-bc-alertcenter.qiniuapi.com
Authorization: <QiniuAdminToken>
```

参数
* id        可选，默认是 id，除非有 alertname
* alertname 可选，有 alertname 字段时，忽略 id
* tags      可选，按照告警分类标签进行过滤，与 alertname 同时存在时，该字段失效
* begin     开始时间
* end       结束时间
* limit     可选 默认为1000 最大为1000
* marker    可选 游标, 分页逻辑会用到 上一次遍历返回的marker字段

返回包
```
200 {
  "items": [
    {
      "id":           "<alertId>",    // 告警 ID
      "key":          "<key>",        // 告警 Key
      "alertname":    "<alertname>",  // 告警名称，例如：pili_vdn_node_uptime
      "desc":         "<desc>",       // 告警的主要信息，比如：域名解析未同步
      "isEmergent":   "<isEmergent>", // 是否告警升级了
      "status":       "<status>",     // 触发状态(选填)，不填则是 firing ，可选值：firing|resolved，
      "severity":     "<severity>",   // 告警级别，可选值：P1(warning) | P0(critical)
      "startsAt":     "<startsAt>",   // 告警发生时间，2016-11-24T00:21:45.887+08:00
      "endsAt":       "<endsAt>",     // 告警结束时间，2016-11-24T00:21:45.887+08:00
      "generatorURL": "<url>",        // 可选，title 的超链接如果有的话，必须带有http或者https的开头
      "labels": {                     // 附加信息(选填)，KV 形式
        "<key>":      "<value>"  
      }
      "comments": [
        {
          "comment":  "<comment>",    // 备注
          "time":     "<time>",       // 告警发生时间
          "username": "<name>"        // 处理责任人
        },
        ...
      ]
    },
    ...
  ],
  "marker": "<marker>" // 下一次遍历请求需要带上该字段
}
```

### 2 AlertProfile 相关
#### 2.1 创建告警 Profile
请求包
```
POST /alerts/profiles
Host: pili-bc-alertcenter.qiniuapi.com
Authorization: <QiniuAdminToken>
{
  "alertname":    "<alertname>",
  "description":  "<description>",
  "tags":         ["<tag1>", "<tag2>"]
  "needOncall":   "<needOncall>",
  "notifiers":    ["<notifier1>", "<notifier2>"]
}
```

返回包
```
200 {}
```

#### 2.2 获取所有的告警 Profile
请求包
```
GET /alerts/profiles
Host: pili-bc-alertcenter.qiniuapi.com
Authorization: <QiniuAdminToken>
```

返回包
```
200 [  
  {
    "alertname":    "<alertname>",
    "description":  "<description>",
    "tags":         ["<tag1>", "<tag2>"],
    "needOncall":   "<needOncall>",
    "notifiers":    ["<notifier1>", "<notifier2>"],
    "isNew":        "<isNew>",
    "createAt":     "<createAt>",
    "latestTime":   "<latestTime>",
    "updateAt":     "<updateAt>"
  },
  ...
]
```


#### 2.3 获取某一个告警的 Profile
请求包
```
GET /alerts/profile?alertname=<alertname>
Host: pili-bc-alertcenter.qiniuapi.com
Authorization: <QiniuAdminToken>
```

返回包
```
200 {
  "alertname":    "<alertname>",
  "description":  "<description>",
  "tags":         ["<tag1>", "<tag2>"],
  "needOncall":   "<needOncall>",
  "notifiers":    ["<notifier1>", "<notifier2>"],
  "isNew":        "<isNew>",
  "createAt":     "<createAt>",
  "latestTime":   "<latestTime>",
  "updateAt":     "<updateAt>"
}
```

#### 2.4 修改告警 Profile
请求包
```
POST /alerts/profiles/update
Host: pili-bc-alertcenter.qiniuapi.com
Authorization: <QiniuAdminToken>
{
  "alertname":    "<alertname>",
  "description":  "<description>",
  "tags":         ["<tag1>", "<tag2>"]
  "needOncall":   "<needOncall>",
  "notifiers":    ["<notifier1>", "<notifier2>"],
  "isNew":        "<isNew>"
}
```

> 该 API 操作属于全量更新操作，每次调用都覆盖原来的值

返回包
```
200 {}
```

#### 2.5 给告警打分类标签
请求包
```
POST /alerts/profiles/tags
Host: pili-bc-alertcenter.qiniuapi.com
Authorization: <QiniuAdminToken>
{
  "type":       "<append|delete>",
  "alertnames": ["<alertname1>", "<alertname2>"],
  "tags":       ["<tag1>", "<tag2>"]
}
```

> 该操作不是全量跟新，是属于追加/移除操作

返回包
```
200 {}
```

#### 2.6 修改告警名称
请求包
```
POST /alerts/profiles/rename
Host: pili-bc-alertcenter.qiniuapi.com
Authorization: <QiniuAdminToken>
{
  "old": "<alertname>",
  "new": "<alertname>",
}
```

> 该操作属于高危操作，请注意使用

> 如果修改后的告警名称 (New) 已经有的话，已经有的不会有变动，老的会被移除掉，key 会重新进行计算


返回包
```
200 {}
```

#### 2.7 删除告警 Profile
请求包
```
DELETE /alerts/profiles?alertname=<alertname>
Host: pili-bc-alertcenter.qiniuapi.com
Authorization: <QiniuAdminToken>
```

> 该操作属于高危操作，请注意使用

> 该操作会把与之相关所有的告警都删除掉

返回包
```
200 {}
```

#### 2.8 获取所有的通知方式
请求包
```
GET /alerts/notifiers
Host: pili-bc-alertcenter.qiniuapi.com
Authorization: <QiniuAdminToken>
```

返回包
```
200 ["notifier1", "notifier2"]
```


### 3 告警事件相关 API

#### 3.1 将告警设置为在处理中
请求包
```
POST /alerts/ack
Host: pili-bc-alertcenter.qiniuapi.com
Authorization: <QiniuAdminToken>
{
  "ids":            ["<alertId>", ...],   // 默认是 id，除非有 alertname
  "alertnames":     ["<alertname>",...],  // 有 alertname 字段时，忽略 id 数组
  "comment":        "<comment>",          // 处理时的备注
  "username":       "<username>"          // 处理责任人
}
```

返回包
```
200 {}
```
