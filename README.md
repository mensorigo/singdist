# SubDist 面板（重构）

sing-box 订阅分发面板的模块化重构。开发分离、部署合并：前端（React +
TypeScript + Vite + Tailwind + shadcn/ui）独立构建，产物嵌入 Go（Gin）
后端，最终交付单个二进制。设计文档见 [ARCHITECTURE.md](./ARCHITECTURE.md)。

> 当前进度：**里程碑 1** — 登录页、仪表盘、左侧边栏（含暗色模式）。
> 订阅 / 入站 / 证书 / 设置为占位页，后续里程碑实现。

## 构建

```bash
./build.sh        # 构建前端 -> 拷入 server/internal/httpx/webdist -> go build
# 产物： ./bin/subdist
```

单独开发时也可以只构建后端（仓库内附了一个占位 index.html，
`cd server && go build ./...` 可直接通过）。

## 本地运行（假核心 + 自签证书）

```bash
SUBDIST_CORE=fake \
SUBDIST_DATA_DIR=/tmp/subdist-data \
SUBDIST_ADDR=127.0.0.1:8443 \
./bin/subdist
```

然后浏览器打开 <https://127.0.0.1:8443>（自签证书，浏览器需手动信任一次；
`curl` 测试加 `-k`）。

只跑后端做 API 调试时，用 Vite 开发服务器可获得热更新：

```bash
cd web && npm run dev   # /api 已代理到 https://localhost:8443（secure:false）
```

## 首次启动与账号

- 首次启动（数据库为空）会创建管理员：用户名固定 `root`，
  **密码随机生成并打印在启动终端**，用横幅框出，只打印这一次。
- 密码以 bcrypt 哈希存储（cost 10），数据库中没有明文；
  之后启动不再打印密码。忘记密码只能清库重来（或后续里程碑的重置工具）。
- 会话为服务端 Session：Cookie 名 `subdist_session`，
  带 `HttpOnly + Secure + SameSite=Lax`，有效期 7 天，库里只存 token 的
  SHA-256 哈希。登录失败 5 次锁定 5 分钟（按用户名+IP）。
- 改密码接口已就绪：`PATCH /api/auth/password`
  （需登录，body：`{"currentPassword": "...", "newPassword": "..."}`）。
  改用户名已预留接口结构，后续里程碑开放。

## 配置（环境变量）

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `SUBDIST_DATA_DIR` | `./data` | 数据库与自签证书存放目录 |
| `SUBDIST_ADDR` | `:8443` | HTTPS 监听地址 |
| `SUBDIST_CORE` | `clash` | `clash`（官方 sing-box Clash API）/ `fake`（假数据，本地开发） |
| `SUBDIST_CLASH_API` | `http://127.0.0.1:9090` | Clash API 地址 |
| `SUBDIST_CLASH_SECRET` | 空 | Clash API 密钥（若核心设置了 secret） |
| `SUBDIST_TLS_CERT` / `SUBDIST_TLS_KEY` | 空 | 正式证书路径；为空时在数据目录自动生成自签证书 |

示例文件：`server/.env.example`。

**全局加密**：面板只提供 HTTPS，没有明文模式。未提供正式证书时，
首次启动会在数据目录生成一张含 `localhost / 127.0.0.1` SAN 的自签证书。

## 后端模块划分（严格独立，接口通信）

```
server/
├─ cmd/subdist/        # 仅装配：配置、模块构造、路由挂载
└─ internal/
   ├─ store/           # 唯一碰 SQL 的包：SQLite(WAL) + 内嵌顺序迁移
   ├─ auth/            # 账号引导、登录限流、会话、改密；只依赖 Repository 接口
   ├─ core/            # 核心适配层：CoreClient 接口（Clash API 实现 / fake 实现）
   ├─ dash/            # 仪表盘采样器（2s 轮询核心）+ /proc 系统信息
   ├─ httpx/           # Gin 引擎、日志/恢复中间件、会话守卫、静态嵌入、HTTPS
   └─ apix/            # 全站统一错误体 {code,message} 与上下文键
```

核心适配只面向**官方 sing-box**（Clash API），不依赖自编译包才有的
v2ray API；官方核心不可达时仪表盘降级为 `coreOnline=false` 并给出提示。

## 主要接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/auth/login` | 登录（公开） |
| GET | `/api/auth/status` | 会话状态（公开） |
| POST | `/api/auth/logout` | 退出（需登录） |
| PATCH | `/api/auth/password` | 修改密码（需登录） |
| GET | `/api/dashboard/summary` | 仪表盘汇总：速率/累计/连接/曲线/系统（需登录） |
