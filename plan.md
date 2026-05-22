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
- Agent：Go，拉取 desired config，写入 `.runtime/agent/{nodeID}/sing-box.json`，mock 上报用户流量。
- 前端：Vite + React + TypeScript，包含仪表盘、用户、节点、订阅预览。
- 协议：AnyTLS 为主，Shadowsocks 为辅。
- 拓扑：用户连接 Entry；协议实际跑在 Exit；端口转发仅建模，不自动配置系统服务。
- 统计：按 V2Ray API 的用户统计模型设计，本地 MVP 使用 mock collector。
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

## Next Steps

- 给 Agent 增加真实 V2Ray API stats collector，并保留 mock collector 作为本地模式。
- 前端补用户额度编辑、节点证书模式编辑、desired config 预览。
- 增加 staging 环境变量和反代示例，例如公网域名、HTTPS、备份 SQLite volume。
