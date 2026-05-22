# sing-box 多节点面板 MVP 开发计划

## Summary

本项目从空目录开始实现一个 Go + React 的 sing-box 多节点面板 MVP。第一版聚焦本地闭环：SQLite 保存用户、Entry/Exit 节点、流量账本；API 生成 Exit 端 sing-box AnyTLS/Shadowsocks 配置；订阅接口生成用户 sing-box JSON；Agent 先用 mock stats 上报流量，为以后接真实 sing-box V2Ray API 留接口。

参考：

- AnyTLS inbound: https://sing-box.sagernet.org/configuration/inbound/anytls/
- V2Ray API: https://sing-box.sagernet.org/configuration/experimental/v2ray-api/
- ACME provider: https://sing-box.sagernet.org/configuration/shared/certificate-provider/acme/
- DNS01 challenge: https://sing-box.sagernet.org/configuration/shared/dns01_challenge/
- Build from source / tags: https://sing-box.sagernet.org/installation/build-from-source/

## MVP Scope

- 后端/API：Go `net/http` + SQLite。
- Agent：Go，拉取 desired config，写入 `.runtime/agent/{nodeID}/sing-box.json`，支持 mock 和 V2Ray API stats collector。
- 前端：Vite + React + TypeScript，包含仪表盘、用户、节点、订阅预览。
- 协议：AnyTLS 为主，Shadowsocks 为辅。
- 拓扑：用户连接 Entry；协议实际跑在 Exit；端口转发仅建模，不自动配置系统服务。
- 统计：按 V2Ray API 的用户统计模型设计；默认使用 mock collector，本地/真实环境可切到 `v2ray-api`。
- 部署：API 可以可选托管 `apps/web/dist`，Docker 形态为 API/Web/SQLite volume 单容器。
- sing-box 编译：参考官方 `release/DEFAULT_BUILD_TAGS*` 和 `release/LDFLAGS`，并额外确保包含 `with_v2ray_api`。

## API

- `GET /api/health`
- `GET /api/summary`
- `GET/POST /api/users`
- `PATCH /api/users/{id}`
- `GET/POST /api/exit-nodes`
- `PATCH /api/exit-nodes/{id}`
- `GET/POST /api/entry-nodes`
- `PATCH /api/entry-nodes/{id}`
- `GET /api/subscriptions/{userID}/sing-box.json`
- `GET /api/agent/{nodeID}/desired-config`
- `POST /api/agent/{nodeID}/heartbeat`
- `POST /api/agent/{nodeID}/traffic`

## Data Model

- `users`：名称、启用状态、总流量额度、已用流量、AnyTLS password、SS password、创建/更新时间。
- `exit_nodes`：落地机名称、协议端口、证书模式、证书字段、最后心跳、期望配置版本。
- `exit_nodes` stats 字段：`stats_mode` 控制 `mock`/`v2ray-api`，`stats_api_listen` 控制 sing-box V2Ray API 监听地址。
- `entry_nodes`：线路机名称、公开 hostname/IP、AnyTLS/SS 公开端口、绑定的 Exit。
- `traffic_events`：节点、用户、上行、下行、来源、时间戳。

## Acceptance Criteria

- 本地能启动 API、Agent、Web。
- 能创建 1 个用户、1 个 Exit、1 个 Entry。
- `/api/agent/{nodeID}/desired-config` 返回包含 AnyTLS 和 Shadowsocks inbound 的 sing-box 配置。
- `/api/subscriptions/{userID}/sing-box.json` 返回客户端 sing-box JSON，server 使用 Entry hostname/port。
- Agent 能写入 `.runtime/agent/{nodeID}/sing-box.json` 并 mock 上报流量。
- 超额或禁用用户不会出现在服务端配置和订阅里。

## Progress

- 已完成本地 MVP 骨架：Go API、Go Agent、React 管理页、SQLite 数据库、配置生成、订阅生成。
- 已补 store/configgen/API 集成测试，覆盖用户、Entry/Exit、订阅、desired config、mock traffic 累加。
- Agent 已支持可选 `--check-config`，用于在写入配置后执行 `sing-box check -c`；默认关闭，避免本地无 sing-box 时阻塞开发。
- API 已支持 `--web-dir` 托管前端 build，并对 React 路由提供 `index.html` fallback。
- 已增加 Dockerfile、`deployments/docker-compose.yml`，用于主控 VPS staging 的单容器部署。
- 已增加 `scripts/build-sing-box.ps1`：读取 sing-box 官方默认 build tags/ldflags，再追加 `with_v2ray_api` 编译。
- Exit 节点已支持 `stats_mode` / `stats_api_listen`；`v2ray-api` 模式会在 sing-box config 中生成 `experimental.v2ray_api.stats.users`。
- Agent 已抽象 collector，支持 `--stats-mode auto|mock|v2ray-api`；真实模式通过 gRPC `StatsService.QueryStats` 读取 `user>>>{id}>>>traffic>>>uplink/downlink` 并上报。
- 前端已支持用户名称/额度编辑、Exit 证书模式与证书字段编辑、统计模式编辑，以及按 Exit 预览 agent desired config。
- API 容器已支持 `SING_PANEL_ADDR` / `SING_PANEL_DB` / `SING_PANEL_WEB_DIR` 环境变量默认值，并补齐 staging `.env`、Caddy HTTPS/basic-auth 反代示例和 SQLite volume 备份说明。
- Docker Compose 已在本机实际 build/up 验证通过；空库列表接口固定返回 `[]`，前端改用相对 `/api` 并通过 Vite dev proxy 保持本地开发体验。
- Agent 已支持面板 Basic Auth，并会在写入 sing-box 配置前把 `*_env` 占位字段解析成真实字段；sing-box 构建脚本默认强制包含 `with_v2ray_api` 和 `with_acme`。
- 已增加 Exit host 的 agent systemd 模板和环境变量示例，便于 staging 持久化运行 agent + `sing-box check`。
- Windows 本地已编译带官方默认 tags + `with_v2ray_api` 的 sing-box，并完成本地真实统计烟测：server/client sing-box 通过 Shadowsocks 造流量，agent 从 V2Ray API 采集后用户 `used_bytes` 增长。

## Next Steps

- 用真实编译了 `with_v2ray_api` 的 sing-box 在 staging 上做端到端统计验收。
