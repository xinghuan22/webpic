# image-gateway

Go + Gin 图片元数据解析与流式代理服务。支持 Danbooru、Gelbooru（ID / MD5）、yande.re、Konachan。无需数据库、Node 或磁盘图片缓存，HTML/CSS/JS 内嵌到二进制中。

## 项目目录

```text
cmd/server/           配置读取、HTTP 服务、优雅退出
internal/resolver/    URL 解析、站点 adapter、统一 ImagePost、元数据请求
internal/safehttp/    共享客户端、域名策略、安全 DNS 拨号、重定向限制
internal/cache/       有容量上限的泛型内存 TTL 缓存
internal/proxy/       并发限制、流式图片转发、下载及 Range
internal/manga/       签名清单、漫画阅读器媒体代理与 JM 解混淆
internal/server/      Gin 路由、HTML/JSON 错误响应、请求日志
web/                  embed 资源、暗色响应式模板、CSS、少量 JS
Dockerfile            多阶段构建，scratch + CA，非 root 运行
compose.yaml          nerdctl compose 配置，不映射宿主机端口
```

## 本地启动

要求 Go 1.22 或以上；部署构建镜像使用 Go 1.26。首次构建需要下载 Go 模块。

```sh
go mod download
go run ./cmd/server
```

打开 `http://localhost:8080`，粘贴作品链接。机器人可以调用：

```sh
curl -G http://localhost:8080/api/resolve \
  --data-urlencode 'url=https://danbooru.donmai.us/posts/11640681'
```

编译为单个二进制：

```sh
CGO_ENABLED=0 go build -buildvcs=false -tags=nomsgpack -trimpath -ldflags='-s -w' -o bin/image-gateway ./cmd/server
./bin/image-gateway
```

`-buildvcs=false` 兼容不完整 Git 工作区；竞态检查需要 C 编译器。

`nomsgpack` 去掉不使用的 Gin MessagePack 支持；普通 `go build` 也可用。

## containerd / nerdctl 与 Caddy

Compose 会创建固定名称为 `data_kissnab` 的 bridge 网络，子网为
`172.20.0.0/24`。Caddy 容器需要处在同一个 containerd namespace，并加入该网络：

```sh
nerdctl network ls
cp .env.example .env
mkdir -p data/manga/manifests/jm
chown -R 65532:65532 data/manga
openssl rand -hex 32
```

把最后一条命令生成的值写入 Go 服务 `.env` 的 `MANGA_PUBLISH_SECRET`，并在
AstrBot 插件配置的 `manga_publish_secret` 中填写完全相同的值。两台机器通过
HTTPS 通信，不需要共享目录。现有 Caddy `reverse_proxy image-gateway:8080` 会同时
代理漫画路由，无需增加单独的 handle。

如果 `data_kissnab` 已由其他 Compose 项目创建，请确认其子网也是
`172.20.0.0/24`。启动本项目后，可把运行中的 Caddy 接入网络：

```sh
nerdctl compose up -d --build
nerdctl network connect data_kissnab <Caddy 容器名>
nerdctl compose logs -f image-gateway
```

如果 Caddy 自身也由 Compose 管理，更稳妥的方式是在它的 Compose 文件中把
`data_kissnab` 声明为 external 网络，并将 Caddy 服务加入该网络。构建镜像需要
nerdctl 的 BuildKit 支持。

Caddyfile：

```caddyfile
https://image.lospro.kissnab.top {
    tls /etc/ssl/cert.crt /etc/ssl/cert.key
    reverse_proxy image-gateway:8080
}
```

证书路径指 Caddy 容器内部路径。更新 Caddyfile 后重载已有 Caddy。compose 没有 `ports`，服务仅通过容器网络访问；`EXPOSE` 不会发布公网端口。镜像为只读 scratch，包含 CA 证书，以 UID 65532 运行。默认内存限制 256 MiB，Go 软内存目标 192 MiB，不挂载图片卷。日志轮转为 10 MiB × 3。

## 环境变量

| 名称 | 默认值 | 说明 |
| --- | --- | --- |
| `LISTEN` | `:8080` | 监听地址；compose 固定为容器 8080 |
| `MAX_PROXY_CONCURRENCY` | `16` | 全局媒体并发，范围 1–128，满载返回 429 |
| `METADATA_TTL_SECONDS` | `1200` | 元数据有效期，范围 1–86400 秒 |
| `METADATA_CACHE_MAX` | `1000` | 最大缓存条目，范围 1–10000；MD5 别名也占条目 |
| `GELBOORU_USER_ID` | 空 | Gelbooru 用户 ID |
| `GELBOORU_API_KEY` | 空 | Gelbooru API key，与用户 ID 一起配置 |
| `MANGA_PUBLISH_SECRET` | 空 | AstrBot 与 Go 共用的 HMAC 密钥；至少 32 字符，为空时关闭漫画路由 |
| `PUBLIC_BASE_URL` | `http://localhost:监听端口` | 发布接口返回给用户的公网地址 |
| `MANGA_MANIFEST_DIR` | `data/manga/manifests` | 只保存小型 JSON 清单，不保存漫画图片 |
| `JM_IMAGE_HOST_SUFFIXES` | 内置 JM CDN 列表 | 允许代理的图片域名后缀，逗号分隔 |
| `MAX_MANGA_DECODE_CONCURRENCY` | `2` | 同时解混淆的图片数，范围 1–8 |
| `MAX_MANGA_IMAGE_MIB` | `24` | 单张待解混淆图片的编码大小上限 |
| `MAX_MANGA_MEGAPIXELS` | `40` | 单张待解混淆图片的像素上限 |
| `LOG_LEVEL` | `info` | `info` / `debug` |
| `GOMEMLIMIT` | compose 中 `192MiB` | Go GC 软内存目标，不是硬限制 |

直接运行二进制不会自动读取 `.env`；使用 shell 导出环境变量。`.env` 用于 compose 插值，已被 Git 和 Docker 构建上下文忽略。

## 路由

| 路由 | 行为 |
| --- | --- |
| `GET /` | 链接输入页 |
| `GET /health` | 存活探针，不访问上游 |
| `GET /api/resolve?url=...` | 统一 `ImagePost` JSON |
| `GET /view?url=...` | 图片结果页 |
| `GET /post/:site/:id` | 根据站点与 ID 查看 |
| `GET /media/:site/:id/:variant` | `preview` / `sample` / `original` 流式代理 |
| `GET /download/:site/:id` | 原图附件下载，文件名 `site_id.ext` |
| `POST /api/manga/publish` | AstrBot 使用 HMAC 签名发布 JM 元数据清单 |
| `GET /manga/jm/:albumID` | 纵向懒加载漫画阅读页，浏览器记忆阅读位置 |
| `GET /api/manga/jm/:albumID` | 供阅读页获取脱敏后的章节与代理图片地址 |
| `GET /manga/media/jm/:albumID/:chapterID/:page` | 临时拉取图片并按需解混淆 |

站点标识：`danbooru`、`gelbooru`、`yandere`、`konachan`。

Gelbooru MD5 示例：`https://gelbooru.com/index.php?page=post&s=list&md5=ff3f5c2fbaf68f9b489d234c1fe922fb`。服务器以 `tags=md5:<hash>` 查询，并检查结果 MD5 与输入一致；页面使用解析出的 post ID。

Gelbooru 可能针对服务器出口 IP 要求 API 认证。遇到 Gelbooru 单站持续返回 502 时，
在 `.env` 中同时配置 `GELBOORU_USER_ID` 和 `GELBOORU_API_KEY`，然后重新创建容器：

```sh
nerdctl compose up -d --build --force-recreate
nerdctl compose logs --tail=100 image-gateway
```

`metadata request` 日志中的 `upstream_status` 可用于区分认证失败（通常为 401/403）、
限流（429）和响应解析问题（该值为 0）。日志不会输出 API key。

API 错误结构：`{"error":{"message":"…","id":"…"}}`，使用对应 HTTP 状态码；HTML 错误页显示同一错误 ID，可在 slog JSON 日志中查询。日志不保存输入 URL、上游响应正文、API key、Cookie 或客户端 IP。

## Adapter 实现情况

| 站点 | 接口 / 映射 | 验证状态 |
| --- | --- | --- |
| Danbooru | `/posts/{id}.json`；原图、large、preview、分类 tags、尺寸等 | 离线 mock 已通过；真实 API 待联调 |
| Gelbooru | DAPI JSON；ID / MD5；支持 `post` 包装和数组响应、字符串或数字字段、可选认证；通过 tag DAPI 补充 Artist / Character / Copyright 分类 | 离线 mock 已通过；真实 API 待联调 |
| yande.re | `/post.json?api_version=2&include_tags=1&tags=id:{id}`；使用响应中的 tag 类型补充 Artist / Character / Copyright | 离线 mock 已通过；已通过部署接口验证基础媒体代理 |
| Konachan | 同类 Moebooru 接口；兼容 v1 数组响应和 v2 包装响应 | 离线 mock 已通过；真实 API 待联调 |

开发环境尝试读取四站的真实 JSON 均遇到连接拒绝，浏览工具也未取得接口响应。因此以上不能视为真实站点连通性或当前生产 JSON 格式的保证。测试数据是构造的契约数据，并非冒充线上抓取样本。

参考官方资料：[Gelbooru API](https://gelbooru.com/index.php?page=wiki&s=view&id=18780)、[Moebooru 源码](https://github.com/moebooru/moebooru)、[Danbooru API 文档](https://danbooru.donmai.us/wiki_pages/help:api)。Gelbooru 官方说明可能要求认证或限流；匿名失败时配置上述两项凭据。

## 安全与资源行为

- 输入只接受设计指定的六个站点域名，提取合法 ID / MD5 后重建 API URL；没有任意 URL 代理接口。
- 每个 adapter 明确列出媒体 CDN，采用精确域名匹配。新增 CDN 必须审查后修改对应 adapter，不使用通配符。
- 上游仅允许 HTTPS / 443，禁止 URL 用户信息。每次请求及重定向都重新验证域名，最多跟随 4 次重定向。
- 实际拨号先解析 DNS，拒绝私网、回环、link-local、共享地址和若干保留地址，再直接连接已检查的 IP，避免 DNS 重绑定。保留正常 TLS 主机名验证。不使用环境 HTTP 代理。
- 共享 HTTP client，单主机最多 20 连接，32 个空闲连接；元数据请求并发最多 8，20 秒超时，JSON 读取有上限；媒体请求最多 3 分钟。
- 图片使用 `io.CopyBuffer` / 32 KiB 缓冲，不写磁盘、不读取整张图片到内存。传输中断只记录日志，不能在已输出的图片后附加错误页。
- 漫画图片不做永久缓存。需要解混淆的图片只在 `/tmp` tmpfs 中短暂停留，完成响应后立即删除；清单 API 不返回上游图片 URL 和解码参数。
- 传递 Content-Type、Content-Length、ETag、Last-Modified、Range 与条件请求；图片使用 `private, max-age=300`，不会继承上游 Cookie。
- 拒绝 HTML、SVG 等主动内容，不把上游错误页面作为媒体输出。模板自动转义，页面设置 CSP。
- 页面固定加载 sample，sample 缺失时才回退 preview；只有用户点击“查看原图”或“下载原图”时才请求 original。sample 与 original URL 相同时会跳过，避免页面自动加载原图。

## 测试

普通测试不访问互联网：

```sh
gofmt -w cmd internal web/embed.go
go test ./...
go vet ./...
go build -buildvcs=false ./cmd/server
CGO_ENABLED=1 go test -race ./...
```

覆盖 URL 与 MD5 解析、非法域名/IP、CDN 校验、adapter 数据映射、缓存过期与容量、页面与错误响应、下载文件名、Range、流式读取、并发限制和主动内容拦截。

## 已知限制与扩展

- 四站生产 API 与 CDN 尚待部署环境联调；上游反爬、账号权限、删除内容、限流、CDN 变更都可能导致失败。程序不绕过上游认证、不解析网页寻找原图。
- Gelbooru 的普通 post 响应只有统一 tags，因此会额外调用 tag DAPI 分类 Artist / Character / Copyright；分类请求失败时图片仍可查看，这三个字段暂时显示为空。Moebooru 未提供的分类和文件大小等字段显示未知。
- 不生成缩略图、不转码，不内嵌视频播放器；视频可通过原图入口打开或下载。部分格式取决于浏览器支持。
- 媒体 3 分钟总超时可能中断非常慢的大图下载；客户端可使用 Range 重试。
- 元数据缓存只在单进程内共享，不合并同时发生的相同 cache miss，满额淘汰最早过期条目；增加容量会提高内存占用。
- 并发限制控制正在执行的请求，不是用户配额或带宽计费。公开部署的访问控制可放在已有 Caddy 层。
- 未在本环境实际构建容器或验证 nerdctl / Caddy 联通。无需部署即可运行的 Go 检查结果见交付说明。
- 未来增加站点时实现 `Resolver` 并注册；漫画功能可另加路由和数据结构，不需要修改现有媒体代理协议。
