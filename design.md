请帮我实现一个 Go + Gin 的轻量级图片结果展示与代理服务，项目暂定名 image-gateway。

## 一、运行环境与总体约束

运行环境是一台资源较小的 VPS：

- Linux
- containerd + nerdctl
- 约 1GB RAM
- 25GB 磁盘
- 已有 Caddy 容器作为 HTTPS 入口
- 服务最终运行在 containerd/nerdctl 容器中
- Caddy 会反代到该服务
- 不希望长期保存图片到 VPS
- 不使用 Redis、PostgreSQL、MySQL
- 第一版尽量无数据库
- 优先低内存、低磁盘占用
- Go 使用 Gin
- 尽量少依赖第三方库
- 代码需要易于继续扩展漫画阅读器功能

不要做代理整站镜像。
这个程序只负责：

1. 解析图站 post URL
2. 获取 post 元数据
3. 获取 preview/sample/original 图片地址
4. 代理图片内容
5. 提供美观的图片结果页面
6. 提供原图查看/下载入口

图片必须采用流式转发，禁止使用 io.ReadAll() 把整张原图读入内存。

---

# 二、第一期支持图站

支持：

1. Danbooru
   - danbooru.donmai.us

2. Gelbooru
   - gelbooru.com

3. yande.re

4. Konachan
   - konachan.com

设计必须采用 adapter/resolver 模式，未来方便继续增加：

- Sankaku
- Zerochan
- Anime-Pictures
- 其他 booru
- 漫画站

不要把所有站点逻辑全部堆进 handler。

建议类似：

internal/resolver/
    resolver.go
    danbooru.go
    gelbooru.go
    yandere.go
    konachan.go

---

# 三、输入形式

核心需求是机器人从 IQDB 获得结果 URL 后，把 URL 交给该服务。

例如：

Danbooru：

https://danbooru.donmai.us/posts/11640681

Gelbooru：

https://gelbooru.com/index.php?page=post&s=view&id=14341716

以及 IQDB 有时返回：

https://gelbooru.com/index.php?page=post&s=list&md5=ff3f5c2fbaf68f9b489d234c1fe922fb

yande.re：

https://yande.re/post/show/123456

Konachan：

https://konachan.com/post/show/123456

程序需要自动：

URL
→ 判断站点
→ 提取 post ID 或 MD5
→ 调对应站点 API
→ 得到统一 ImagePost 数据

其中 Gelbooru 的 md5 URL 也必须自动解析，不要要求调用者再人工找 post id。

---

# 四、不要靠解析网页找原图

尽量通过各站 API / JSON 接口直接获取数据。

例如思路：

Danbooru：
GET https://danbooru.donmai.us/posts/{id}.json

关注字段包括：
- id
- file_url
- large_file_url
- preview_file_url
- image_width
- image_height
- file_size
- file_ext
- rating
- score
- source
- tag_string_artist
- tag_string_character
- tag_string_copyright
- tag_string_general
- created_at

Gelbooru：
使用其 DAPI JSON 接口。

需要同时支持：
- post id
- md5 查询

解析：
- file_url
- sample_url
- preview_url
- width
- height
- tags
- rating
- score
- source
等可获得字段。

如果 Gelbooru API 在部分环境要求 user_id/api_key，请设计为可选环境变量：

GELBOORU_USER_ID
GELBOORU_API_KEY

没配置时优先尝试匿名访问。

yande.re / Konachan：
通过 /post.json?tags=id:{id} 一类 API 获取。

不要依赖网页 DOM、CSS selector、XPath 来寻找原图。

---

# 五、统一数据结构

设计一个统一结构，类似：

type ImagePost struct {
    Site         string
    ID           string

    PostURL      string

    PreviewURL   string
    SampleURL    string
    OriginalURL  string

    Width        int
    Height       int
    FileSize     int64
    FileExt      string

    Rating       string
    Score        int
    Source       string

    Artists      []string
    Characters   []string
    Copyrights   []string
    Tags         []string

    CreatedAt    string
}

允许不同站点部分字段为空。

UI、API 和图片代理层都只依赖这个统一结构，而不是直接依赖具体站点返回格式。

---

# 六、HTTP API

至少实现：

## 1. Resolver API

GET /api/resolve?url=<url>

例如：

/api/resolve?url=https%3A%2F%2Fdanbooru.donmai.us%2Fposts%2F11640681

返回统一 JSON：

{
  "site": "danbooru",
  "id": "11640681",
  "post_url": "...",
  "preview_url": "...",
  "sample_url": "...",
  "original_url": "...",
  "width": 1920,
  "height": 1080,
  ...
}

---

## 2. HTML 查看页面

GET /view?url=<url>

显示一个美观的图片结果页面。

也允许内部转换成更干净的 URL，例如：

/post/danbooru/11640681

如果架构合适，可以同时支持两种。

---

# 七、页面 UI

我希望页面整体风格简洁、现代、偏暗色。

桌面布局：

┌───────────────────────────────┐
│            Header             │
├──────────────┬────────────────┤
│              │                │
│   图片信息    │      图片       │
│   左侧栏      │      预览       │
│              │                │
│              │                │
├──────────────┴────────────────┤
│      查看原图 / 下载原图       │
└───────────────────────────────┘

具体要求：

## 左侧

左侧约 280~360px，展示：

- 来源站点
- Post ID
- 图片尺寸
- 文件大小
- 图片格式
- Rating
- Score
- Artist
- Character
- Copyright
- Source
- Tags

Tags 太多时不要撑爆页面：
- 默认折叠
- 可以“展开全部”
- tag 使用小 badge/chip 展示

重要字段视觉上要有层级。

---

## 右侧

显示常规图片。

优先级建议：

sample
→ large
→ preview

不要默认加载原始几十 MB 的图片。

图片要求：

- max-width: 100%
- max-height 根据视口自适应
- object-fit: contain
- 深色背景
- 图片居中
- 支持点击放大或打开预览

页面加载时应该优先展示 sample/preview，而不是 original。

---

## 页面底部

放主要操作：

[查看原图]

[下载原图]

[打开原站]

其中：

查看原图：
走本服务代理，例如：

/media/danbooru/11640681/original

下载原图：

/download/danbooru/11640681

需要设置：

Content-Disposition: attachment

文件名尽量：

danbooru_11640681.jpg

而不是随机名称。

打开原站：
跳转真实 post URL。

---

# 八、图片代理

实现例如：

GET /media/:site/:id/preview
GET /media/:site/:id/sample
GET /media/:site/:id/original

必须：

1. 服务端重新调用 resolver 得到真实图片 URL
2. 服务端向图站/CDN请求
3. 流式转发给客户端

禁止：

io.ReadAll(resp.Body)

应该使用：

io.Copy()

或者 Gin 合适的 streaming/DataFromReader 方式。

要正确透传：

- Content-Type
- Content-Length（存在时）
- Last-Modified（可选）
- ETag（可选）

并合理设置：

Cache-Control

---

# 九、图片上游兼容

不同图站/CDN可能存在：

- Referer 检查
- User-Agent 检查
- 302/301 重定向

HTTP Client 要：

- 自动跟随有限次数重定向
- 配置合理 timeout
- 使用统一 User-Agent
- 根据站点 adapter 可自定义 Referer

例如：

User-Agent:
image-gateway/1.0 (+https://...)

不要无限 redirect。

---

# 十、安全

这一点非常重要。

绝对不要实现：

/proxy?url=https://任意网址

否则会成为开放代理和 SSRF 接口。

媒体代理只能接受：

/media/:site/:id/:type

由服务端 resolver 自己获得目标图片 URL。

另外对 resolver 输入 URL 做 host allowlist：

只允许：

danbooru.donmai.us
gelbooru.com
www.gelbooru.com
yande.re
konachan.com
www.konachan.com

如果上游 API 返回图片 CDN 域名，可以由各 adapter 明确允许对应 CDN。

禁止代理：

- localhost
- 127.0.0.0/8
- ::1
- RFC1918 私网
- link-local
- metadata IP
- 任意用户指定 URL

---

# 十一、资源限制

这是一台 1GB RAM VPS。

要求：

- 不长期缓存图片到磁盘
- 不把整张图片加载到内存
- HTTP Client 复用连接
- 全局复用 http.Client
- 设置连接池上限
- 设置请求 timeout
- 限制并发图片代理数量

建议使用 semaphore，例如最多：

16 或 32 个并发图片代理连接

超过时返回：
429 Too Many Requests

具体数字做成环境变量。

---

# 十二、缓存

第一版不需要磁盘图片缓存。

但 metadata 可以加简单的内存 TTL cache。

例如：

site + postID
→ ImagePost

TTL：
10~30 分钟

最大条目：
500~2000

不要为了缓存引入 Redis。

如果自己实现简单 TTL map 即可。

缓存层要设计独立，以后方便换实现。

---

# 十三、HTML / CSS

不要引入 Vue、React、Node 构建链。

建议：

Go html/template
+
原生 CSS
+
少量原生 JS

静态资源使用：

//go:embed

把 templates/css/js 打进 Go binary。

最终运行时只需要一个二进制。

页面必须响应式。

手机：

图片在上
信息在下
按钮固定清晰可点

桌面：

左信息
右图片

---

# 十四、项目结构

建议：

image-gateway/
├── cmd/
│   └── server/
│       └── main.go
│
├── internal/
│   ├── resolver/
│   │   ├── resolver.go
│   │   ├── danbooru.go
│   │   ├── gelbooru.go
│   │   ├── yandere.go
│   │   └── konachan.go
│   │
│   ├── proxy/
│   │   └── image.go
│   │
│   ├── cache/
│   │   └── cache.go
│   │
│   ├── handler/
│   │   ├── api.go
│   │   ├── view.go
│   │   └── media.go
│   │
│   └── server/
│       └── router.go
│
├── web/
│   ├── templates/
│   │   └── post.html
│   └── static/
│       ├── app.css
│       └── app.js
│
├── Dockerfile
├── compose.yaml
├── go.mod
└── README.md

不要求完全照搬，如果有更自然的 Go 项目组织方式可以调整。

不要过度使用 OOP 风格或无意义的 interface。

interface 只用于确实存在多个实现的地方，例如 Resolver。

---

# 十五、Gin 路由规划

建议：

GET /
GET /health

GET /api/resolve

GET /view
GET /post/:site/:id

GET /media/:site/:id/:variant

GET /download/:site/:id

未来预留：

/manga/*
/api/manga/*

不要现在实现漫画功能，但结构不要阻碍以后添加漫画阅读器。

---

# 十六、Caddy/containerd 部署

程序监听：

0.0.0.0:8080

容器不要暴露公网端口。

与 Caddy 加入同一个 nerdctl/containerd network。

Caddy：

https://image.lospro.kissnab.top {
    tls /etc/ssl/cert.crt /etc/ssl/cert.key
    reverse_proxy image-gateway:8080
}

提供 Dockerfile。

优先 multi-stage build。

运行镜像尽量使用：

scratch

如果 scratch 因证书/时区等不方便，可以使用：

alpine

但优先保持镜像小。

需要 CA certificates，因为程序会访问 HTTPS 上游。

---

# 十七、compose

提供 compose.yaml。

类似：

services:
  image-gateway:
    build: .
    image: image-gateway:latest
    container_name: image-gateway
    restart: always

    environment:
      - LISTEN=:8080
      - MAX_PROXY_CONCURRENCY=16
      - GELBOORU_USER_ID=
      - GELBOORU_API_KEY=

    logging:
      driver: json-file
      options:
        max-size: "10m"
        max-file: "3"

    networks:
      - caddy-network

networks:
  caddy-network:
    external: true

实际 network 名称做成容易修改的形式。

---

# 十八、错误页面

不要把 Go 默认纯文本错误直接给普通用户。

HTML 页面请求失败时提供好看的错误页：

例如：

无法获取图片信息

可能原因：
- 图站暂时不可用
- 图片已删除
- 图站 API 限流
- URL 无效

错误 ID:
xxxxxxxx

API 接口仍然返回标准 JSON error。

---

# 十九、日志

程序日志使用 Go slog。

记录：

- site
- post id
- API 请求耗时
- media proxy 耗时
- HTTP 状态码
- 错误

不要记录：
- 完整 Cookie
- API Key
- 用户隐私信息

支持环境变量：

LOG_LEVEL=info/debug

---

# 二十、测试

至少写：

resolver URL 解析单元测试。

例如：

Danbooru URL
Gelbooru id URL
Gelbooru md5 URL
yande.re URL
Konachan URL

以及基本 SSRF/非法域名测试。

如果上游 API 测试容易受网络影响，使用 httptest mock，不要让普通 go test 必须访问真实互联网。

---

# 二十一、实现原则

优先级：

1. 正确
2. 简单
3. 稳定
4. 易扩展
5. 性能

不要为了“架构漂亮”创建大量无意义抽象。

尤其不要写成：

controller
service
repository
manager
factory
provider
DAO
DTO
VO

多层套娃。

这是一个长期维护的个人项目。

代码应该让一个 Go 开发者几个月后回来依然容易理解。

---

# 二十二、交付要求

请直接创建完整可运行项目，不要只给示例片段。

完成后：

1. 运行 gofmt
2. 运行 go test ./...
3. 运行 go vet ./...
4. 尝试 go build
5. 修复发现的问题

然后给我：

- 项目目录说明
- 如何启动
- nerdctl compose 启动命令
- Caddy 配置
- 环境变量说明
- 目前各图站 adapter 的实现情况
- 已知限制

如果某图站当前 API 实际格式与预期不同，请通过实际接口响应确认后再实现，不要凭印象硬编码字段。