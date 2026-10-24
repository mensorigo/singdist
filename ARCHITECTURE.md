# singbox-ui 新面板 · 架构设计草案（讨论稿 v0.1，2026-10-23）

目标：在新 VPS（192.220.9.199）上重建自研 sing-box 订阅分发面板，复刻旧版功能（见 ../subdist-project-inventory.md），并在设计上根除旧版已知问题（流量采集静默失效、节点心跳不回写、/sub 明文、偶发写锁冲突）。

约束（实测）：服务器内存仅 709MiB、磁盘 8.7G；单管理员使用；sing-box 为上游官方版本不修改；老订阅链接（uuid/token）必须无缝保留。

**硬性前提（2026-10-23 核实）**：官方发布包的构建标签不含 `with_v2ray_api`，没有每用户流量统计（只有 Clash API 的总流量与实时速度）。现网在跑的 sing-box 是用户自编译版（标签含 `with_v2ray_api,with_clash_api`，装在 /usr/bin/sing-box；官方原包在 backups 里）。新面板的每用户流量采集依赖自编译版；升级 sing-box 时切勿误装官方包，部署检查清单须校验二进制标签，面板健康位须能识别“统计接口缺失”并标红。

## 1. 总体形态

开发分离、部署合并的单体应用（modular monolith）：

```
仓库（monorepo）
├─ web/     前端工程（独立构建）
├─ server/  Go 后端（API + 订阅分发 + 采集器 + 配置生成）
├─ agent/   远程节点 agent（二期）
└─ migrations/ 数据库迁移脚本

部署产物：单个 Go 二进制（go:embed 内嵌前端构建产物）
         + SQLite 数据库文件 + systemd unit
```

数据流：

```
浏览器 ──HTTPS──> singbox-ui(:32434) ──REST──> Clash API(127.0.0.1:9090)
                      │ └──────────gRPC──> v2ray Stats(127.0.0.1:10085)
                      ├─ 生成配置 → 校验(sing-box check) → 原子替换 → 重启/重载 sing-box
                      └─ SQLite(WAL)：订阅/节点/入站/模板/流量/日志
客户端 ──HTTPS──> /sub/:token ──> 按模板渲染 Clash / sing-box(jso) 订阅
远程节点（二期）：agent 主动 HTTPS 上报心跳与每用户计数，主控只存令牌哈希
```

## 2. 技术栈（推荐方案）

| 层 | 选型 | 理由 |
|---|---|---|
| 后端语言 | Go（最新稳定版） | 单二进制、内存占用小（适合 709MiB 机器）、与一代同栈、gRPC/并发采集天然顺手 |
| Web 框架 | Gin（默认；Echo/Chi 备选） | 路由 + 中间件生态（日志、Recovery、校验绑定）现成，开发效率与可维护性更好；框架本身轻量，内存可控 |
| 数据库 | SQLite（modernc 纯 Go 驱动，无 cgo）+ WAL + busy_timeout | 单机单管理员足够；零运维；旧版同路线。明确不选 PostgreSQL（单机场景徒增一个常驻服务和内存开销） |
| 迁移 | 版本号 + 顺序 SQL 迁移文件，启动时自动执行 | 替代旧版的隐式建表，升级可控、可回滚 |
| 前端框架 | React + TypeScript + Vite | 与一代同栈（旧 bundle 为 React+Tailwind），组件生态成熟，构建产物小 |
| UI | Tailwind CSS + shadcn/ui（已定） | 基于 Radix 的组件集，轻、可控、源码在自己仓库里可改；与一代 zinc 配色观感延续 |
| 前端状态/请求 | fetch + TanStack Query（或极简手写 hook） | 轮询/缓存/重试是面板高频需求；图表用轻量库（recharts 或手绘 SVG） |
| 实时数据 | SSE（Server-Sent Events）推送实时网速/连接，普通页面仍走 REST | 一代 1 秒轮询全站很浪费；SSE 单向推送简单、Go 标准库可实现、断线自动重连。仪表盘可保留低频轮询兜底 |
| API 风格 | REST JSON，路径沿用 /api/* 习惯 | 与旧版一致，迁移期可逐项对照验证 |
| 鉴权 | 服务端会话（HttpOnly Cookie）+ bcrypt 口令 + 登录失败锁定 | 沿用一代已验证的做法；节点令牌只存哈希 |
| 构建发布 | 本地/构建机完成前端与后端构建 → go:embed 打包 → scp 上服务器 → systemd 重启；保留 PANEL_DIST 外部目录覆盖开关 | 服务器上不装 Node/Go 工具链（省内存磁盘）；回滚 = 换回旧二进制 |

## 2.5 基础库清单（框架之外同样要引入的专注库）

“纯标准库”不现实：下列能力要么用库、要么自己造轮子，选型以小而专为准。

| 能力 | 选型 | 说明 |
|---|---|---|
| 结构化日志 | 标准库 log/slog + 文件轮转小库（如 lumberjack） | slog 已是标准库内置，JSON 结构化、级别与字段齐全；不够时再换 zap |
| 异常兜底 | Gin Recovery 中间件 + 自定义错误处理中间件 | Go 无异常机制，用错误值；panic 由 Recovery 兜住并记日志，业务错误统一包装错误码与消息 |
| 参数校验 | go-playground/validator | Gin 默认集成，结构体标签校验，是 Go 生态事实标准 |
| 数据库驱动 | modernc.org/sqlite | 纯 Go 无 cgo，交叉编译与单二进制部署简单 |
| gRPC 客户端 | google.golang.org/grpc | 读 sing-box v2ray Stats（一代同款依赖） |
| 口令哈希 | golang.org/x/crypto/bcrypt | 一代同款做法 |
| ID/令牌 | google/uuid + crypto/rand | 订阅 uuid 与会话/节点令牌 |
| YAML 生成 | gopkg.in/yaml.v3 | 渲染 Clash 订阅配置 |
| 二维码 | 轻量 QR 库（如 skip2/go-qrcode） | 订阅链接二维码（前端生成亦可，二选一在开发期定） |

## 3. 后端模块划分（包级）

- `auth`：登录、会话、锁定、口令修改
- `store`：SQLite 访问层与迁移（唯一碰 SQL 的地方）
- `subs`：订阅 CRUD、状态机（active/paused/expired/over-quota 自动停用）
- `render`：订阅渲染（Clash YAML / sing-box JSON），按模板+分组生成，渲染结果可预览、可与旧版做快照对比测试
- `sbconfig`：sing-box 配置生成（结构体序列化而非字符串模板）→ `sing-box check` 校验 → 原子写 → 记录哈希与 sync_log → 失败自动回滚上一份
- `collector`：流量采集（见第 4 节，重点防旧坑）
- `live`：Clash API 代理（实时连接列表/断开、总流量），SSE 推送
- `ipmon`：**IP 检测（用户明确要求）** — 记录“哪些源 IP 用过哪些订阅”，双向可查。两个采集源：① sing-box 连接日志解析（日志行带用户名与源 IP，是真实使用记录；采集器按水位增量解析，健康状态纳入采集健康位）；② /sub 拉取请求记录请求方 IP（导入者）。存储：ip_events 明细（保留 90 天自动清理）+ subscription_ips 汇总（订阅×IP：首次出现、最近出现、次数）。功能：订阅详情页 IP 清单（可与实时连接交叉标“当前在线”）、全局按 IP 反查它用过的所有订阅、一键拉黑、IP 数上限治理与异常提醒。黑名单下发为 sing-box 拒绝规则
- `tlsmgr`：证书查看/上传/回滚（替换后重启自身），证书到期提醒
- `settings`：键值设置，带 schema 校验与默认值
- `nodes`：节点/入站模型（一期仅本机节点，字段与接口按多节点预留）

## 4. 流量采集设计（针对旧版冻结事故）

采集模式做成适配层可切换（待用户定）：① v2ray_api 精确模式（需自编译 sing-box，下述主设计）；② 官方包降级模式 — 分入站用 nftables 按端口计数（精确），分用户用 Clash API 连接轮询 + 源 IP 归属估算（展示级，不能作配额依据），或一人一端口 + 内核计数换精确（牺牲共用 443 形态）。实测（2026-10-23）：官方能力的 Clash /connections 元数据无用户字段，只有源 IP 与入站名，故官方包下不存在无损的分用户采集。

- 每 30 秒经 gRPC 读 sing-box 每用户累计计数（user>>>名称>>>traffic>>>uplink/downlink）
- 增量入账：used += max(0, now - last_raw)；检测到 now < last_raw 判定 sing-box 重启，重置基线并记日志，绝不负增长或丢账
- 每次采集落一条健康记录：成功/失败原因/耗时；连续失败 ≥3 次将仪表盘健康位标红并写日志，**禁止静默失败**
- traffic_samples（每分钟）与 daily_totals（按面板时区日切）只从已入账数据汇总生成
- 节点心跳：采集成功即回写 nodes.last_seen_at 与 sing-box 版本（读 Clash API /version），一期本机节点同样走这条路，杜绝旧版“恒为 0”

## 5. 数据库设计要点

领域口径（用户 2026-10-23 确认并修正）：**一个链接就是一个订阅，但一个订阅在实际使用中会被很多人、很多设备同时导入使用**。因此：订阅是最小管理/统计单位（一个订阅 = 一条链接 = 核心内一个 user 条目/uuid），流量以订阅为口径归集总额；**订阅内部的人/设备在核心层不可区分**（同一 uuid），任何方案都做不到订阅内分人统计，最多按源 IP 近似拆分展示。治理手段是订阅级管控：IP 数上限、连接数上限、订阅的 IP 使用清单、异常 IP 数告警、链接重置（token 轮换）。一代已有 max_ips / limit_conn / IP 收集等雏形，新面板保留并强化。



沿用一代已验证的表结构（订阅/节点/入站/模板/分组/设置/采样/每日/IP 事件/IP 汇总/黑名单/同步日志/会话），修正三点：
- subscriptions 的额度改为明确语义（单一总上限 limit_bytes，沿用一代现行语义，避免上下行双额度歧义）
- 新增 collector_health、config_versions（每次生成的配置留档，可回滚到任意一版）
- 所有时间统一存 Unix 秒，展示层按面板时区转换

## 6. 迁移与上线

1. 迁移工具读旧 subdist.db：订阅（uuid/token 原值）、模板、分组、入站 params、设置、历史流量与 IP 事件按需搬运
2. 影子运行：新二进制在备用端口（如 32435）+ 独立库副本运行，对同一 sing-box 只读验证：订阅渲染与旧版逐字节/逐字段对比、采集数值真实增长
3. 切换：停旧 → 新二进制接管 :32434 与正式库 → 跑测试清单（登录、拉订阅、计数增长、超额停用、黑名单、证书页、重启恢复）
4. 回滚：旧二进制与旧库原地保留，切换后观察期（建议 7 天）内可一条命令切回

## 6.5 开源面板调研结论（2026-10-24，3x-ui / S-UI / Marzban / Hiddify 入站管理）

全文在 ~/workspace/research_notes/open-source-panel-inbound-management-20261024-0203/report.md。与本项目相关的结论：

- 已采纳：**端口唯一性按传输层计算**（迁移 0004 已落地）—(protocol, port) 唯一，VLESS(TCP) 与 Hysteria2(UDP) 可共用同一端口号；同协议同端口才冲突（学 3x-ui 的 TCP/UDP 位掩码思路）。
- 已验证方向一致：配额/到期/启停记在订阅（客户端）侧、入站 users 列表在落地时生成；身份字段用正经列 + 协议参数 JSON 的混合存储 — 四家殊途同归，本项目现行做法与之相同。
- 待定（订阅期再议）：**Reality/TLS 参数是否独立为可复用对象**（S-UI 的 tls_id 模型：一套 Reality 配置被多个入站引用、改一处全生效、链接随之再生成）。当前实现是写进入站 params，入站少时无差别；若将来多入站共用同一伪装配置再迁移。
- 待定（订阅期预留）：**监听与对外宣称解耦**（Marzban Hosts / S-UI addrs)：生成订阅链接时可覆写地址/端口/SNI，与入站的实际监听参数分离。
- 同步引擎（二期）蓝本：S-UI 的事务式保存 + 坏配置自动回滚（旧核心继续跑）+ 3x-ui 的结构体校验与“配置无变化不重启”；与本项目原计划的「生成 → sing-box check → 原子替换 → 健康检查 → 回滚 + config_versions 留档」一致，实施时参照其源码。

## 6.6 订阅分发（2026-10-24 落地）

- 独立包 `internal/render`（纯读路径）：订阅 + 模板入站 + 全局设置 → 节点列表 → Clash YAML / sing-box JSON 两种渲染；公开端点 `GET /sub/:token`（令牌即凭证，按订阅派生状态门控：disabled/expired/over_quota → 403），管理端 `GET /api/subscriptions/:id/preview`（无视状态，供面板预览）。
- 凭证语义：vless/tuic 节点用**订阅的 uuid**；密码类协议（hy2/ss/anytls/trojan）用**入站的共享密码** — 与当前核心配置的 users 形态一致；按订阅生成 users 是同步引擎期的事。
- 节点地址 = 设置「对外地址」，留空取请求 Host；节点名 = 入站名，设置「节点名前缀」非空时拼前缀，重名加 -2/-3 后缀。Clash 组名固定 `Proxy`；响应头带 Profile-Title / Profile-Update-Interval / Subscription-Userinfo（受设置开关控制）。
- 与旧面板输出对齐（2026-10-24 抓取旧 /sub 实样比对）：DNS fake-ip + 兜底集随设置切换、STUN 双端口规则随策略、private/CN 直连、MATCH 兜底。旧面板「同端口明文 HTTP /sub」的做法不复制，统一走面板 HTTPS。

## 6.7 同步引擎（2026-10-24 落地）

- 独立包 `internal/coresync`：按面板期望状态（启用的入站 + 派生状态为 active 的订阅 users）全量生成 sing-box 配置 → 与目标文件哈希比对（无变化不动作）→ 临时文件 `sing-box check` 校验 → 原子替换 → （配置了核心 unit 时）重启 + 健康检查（is-active + Clash API /version），失败自动恢复旧配置再重启。每次尝试记 `sync_logs`（迁移 0007）。
- **2026-10-24 移除**：旧配置存档（`config_versions` + `<dataDir>/config-archive/`）与回滚功能已删除 —— 每次同步存一份，自动同步间隔一旦设短就会堆积存档数据，用户认为没必要。顶栏只剩手动同步按钮 + 状态指示灯；旧表与旧存档文件留在服务器上不再读写。
- 安全形态：写目标与重启 unit 都是**配置项不是常量** — `SINGBOX_UI_SYNC_CONFIG`（默认 = 读路径的 `SINGBOX_UI_SINGBOX_CONFIG`）、`SINGBOX_UI_CORE_UNIT`（默认空 = 只写文件+校验、绝不重启）、`SINGBOX_UI_SINGBOX_BIN`。影子实例指向沙箱文件且不设 unit，可端到端演练而不碰生产核心；正式接管时才指向真实配置 + sing-box unit。
- 生成要点：骨架与现行配置同构（log / clash_api 127.0.0.1:9090 / direct 出站）；Reality 带 handshake（SNI:443）；TLS 密码协议（hy2/tuic/anytls/trojan）内嵌证书管理中的**当前启用证书** PEM；users 语义与分发一致（vless/tuic 用订阅 uuid，密码协议用入站共享密码、按订阅列 users）。
- 面板：顶栏同步控件只剩状态点 + 手动同步按钮（同步二次确认）；历史版本下拉与回滚已随存档功能一并删除。
- 自动同步（2026-10-24，用户定的 10 分钟兜底）：`SINGBOX_UI_AUTO_SYNC_MINUTES`（0 = 关闭）开启进程内定时器；每间隔先比对期望/目标哈希，一致则静默跳过（不写日志、不重启），不一致才走完整管线，日志 action 记为 `auto-sync`。手动同步保留，与定时器共用同一互斥锁。副作用：保存后的改动最迟 10 分钟自动生效，等于 10 分钟反悔窗口；过期/超额订阅也会在 10 分钟内被踢出核心。

## 7. 待商量决策点

1. 后端语言 Go + 框架 Gin（均已定，2026-10-23）
2. 前端：React+TS + Tailwind + shadcn/ui（已定）
3. 模块化要求（用户定）：不同模块之间、服务之间全部独立分开编写，包级解耦、接口通信，禁止跨模块直接摸对方内部实现
4. 实时通道：SSE（推荐）/ 继续全站轮询？（未定，里程碑1 仪表盘先用轮询/SSE 均可，开发期定）
5. 仓库形态：monorepo 单仓（推荐）/ 前后端双仓？（未定，暂按单仓）

## 8. 里程碑 1（用户 2026-10-23 定，先行范围）

- 页面只做三个：**登录页 UI、仪表盘、左侧边栏**（订阅/入站/证书/设置在边栏占位，页面后续迭代）；边栏带**暗色模式**切换
- 登录：用户名固定 `singbox-ui`；密码**首次启动随机生成并打印到终端**，仅打印这一次；bcrypt 哈希存储，库里不存明文；预留后期修改用户名/密码的接口与数据结构
- 全局加密连接：面板全程 HTTPS（本地开发用自签证书，部署用正式证书）；会话 Cookie 带 Secure + HttpOnly
- 核心已于 2026-10-23 按用户指令换回**官方原版 sing-box**（配置去掉 v2ray_api 段，原配置备份在服务器 backups，官方 check 已过、服务验证正常）；新面板一切能力按官方包设计（Clash API、日志解析 IP 检测），不再依赖 v2ray_api
