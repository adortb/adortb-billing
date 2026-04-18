# adortb-billing
> 广告平台充值/扣费/媒体方结算/提现计费服务

## 快速理解

本服务是广告平台的**资金核心**，处于事件流水线的下游。核心职责分两条链路：

1. **广告主链路**: 广告事件（曝光/点击）到达 → Kafka 消费 → Redis Lua 原子扣费 → 余额不足时回落 PG 行锁 → 记录交易流水
2. **媒体方链路**: 扣费成功后记录 pending 收入 → 每日结算任务（UTC 01:00）将 pending 转为 settled → 媒体方申请提现 → 管理员审批 → paid

关键设计决策：
- **双层扣费机制**: Redis 处理高频扣费（Lua 原子操作防超卖），Redis 故障时自动降级到 PG 行锁，保证不丢单
- **32 分片 Pacing**: 将 campaign 按 ID 哈希到 32 个锁分片，大幅降低预算节奏计算的锁竞争
- **状态机结算**: 媒体方收入三态流转，提现五态审批，均通过数据库事务保证原子性

## 目录结构

```
adortb-billing/
├── cmd/billing/main.go                 # 入口：读取 Vault 配置、初始化 PG/Redis/Kafka/OTEL、启动 HTTP
├── internal/
│   ├── account/model.go               # 核心数据结构：AdvertiserAccount, PublisherAccount, Transaction
│   ├── advertiser_billing/service.go  # 充值入账、事件扣费（双层机制）、流水查询
│   ├── publisher_billing/service.go   # 收入记账、结算单生成、提现申请
│   ├── platform/
│   │   ├── service.go                 # 平台日汇总聚合
│   │   └── settlement_job.go          # 定时结算任务：pending → settled（UTC 01:00）
│   ├── consumer/event.go              # Kafka Consumer Group "adortb-billing"，消费 adortb.events
│   ├── api/handler.go                 # HTTP Handler，路由注册，请求参数校验
│   ├── metrics/metrics.go             # Prometheus Counter/Histogram：扣费次数、金额、延迟
│   ├── pacing/                        # 预算节奏控制
│   │   ├── tracker.go                 # 32 分片 Tracker，读写分离
│   │   └── learner.go                 # 流量曲线学习，动态调整节奏因子
│   ├── repo/                          # 数据访问层
│   │   ├── advertiser_repo.go         # 广告主账户 CRUD
│   │   ├── publisher_repo.go          # 媒体方账户 CRUD
│   │   └── transaction_repo.go        # 交易流水读写
│   └── tracing/tracing.go             # OTLP HTTP exporter 初始化（指向 Jaeger）
└── migrations/
    ├── 001_billing.up.sql             # advertiser_accounts, publisher_accounts, transactions
    └── 002_multitenant.up.sql         # 为所有表添加 tenant_id 列与索引
```

## 核心概念

### 广告主账户扣费流程

```
Kafka 事件
    │
    ▼
Redis DECRBY (Lua 原子)  ──成功──▶  写 PG 交易流水
    │失败/余额不足
    ▼
PG BEGIN
  SELECT ... FOR UPDATE  (行锁)
  CHECK balance >= amount
  UPDATE balance
  INSERT transaction
COMMIT
```

### 媒体方账户三层状态

```
广告事件触发
    │
    ▼
revenue_pending     ← 每次计费记入，未确认
    │ 每日结算任务（UTC 01:00）
    ▼
revenue_settled     ← 已核算，可申请提现
    │ 媒体方发起 withdraw
    ▼
revenue_withdrawn   ← 提现完成
```

### 提现审批状态机

```
pending ──管理员审批──▶ approved ──打款完成──▶ paid
   │                       │
   └──拒绝──▶ rejected      └──撤销──▶ cancelled
```

### Pacing Tracker（32 分片）

```
campaign_id % 32  →  选择分片锁
分片内: 读取当日消耗曲线  →  计算节奏因子 [0.0, 1.0]
节奏因子缓存在 Redis，TTL 60s
ADX Core 拉取因子控制出价频率
```

## 开发指南

### 环境准备

```bash
# 启动本地依赖（参考 docker-compose，如有）
docker-compose up -d postgres redis kafka vault jaeger

# 设置环境变量（本地开发）
export PG_HOST=localhost PG_USER=billing PG_PASSWORD=xxx PG_DBNAME=adortb_billing
export REDIS_ADDR=localhost:6379
export KAFKA_BROKERS=localhost:9092
export VAULT_ADDR=http://localhost:8200 VAULT_TOKEN=dev-token
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
export DEPLOYMENT_ENV=development
```

### 运行与测试

```bash
go mod tidy
go test ./...                        # 全量单元测试
go test ./internal/advertiser_billing/... -v  # 指定包
go build -o bin/billing ./cmd/billing
./bin/billing                        # 启动，监听 :8085
```

### 新增业务逻辑规范

1. **数据访问**: 所有 SQL 操作放在 `internal/repo/`，service 层禁止直接操作 DB
2. **扣费操作**: 必须先走 Redis Lua 路径，回落逻辑统一封装在 `advertiser_billing/service.go`
3. **状态变更**: 涉及账户余额或结算状态的变更，必须在数据库事务内完成
4. **指标上报**: 扣费、结算等关键操作在 `metrics/metrics.go` 中注册并上报
5. **测试覆盖**: 每个 service 方法须有对应单元测试，覆盖正常路径与错误路径

### 添加新 API 端点

1. 在 `internal/api/handler.go` 注册路由
2. 在对应 service 层实现业务逻辑
3. 在 `internal/repo/` 实现数据访问
4. 在 `metrics/metrics.go` 添加相关指标
5. 编写单元测试

## 依赖关系

### 上游（输入）

| 来源 | 协议 | Topic / 说明 |
|------|------|-------------|
| ADX Core | Kafka | `adortb.events`（曝光/点击/转化事件） |
| Event Pipeline | Kafka | 同上，经 Pipeline 清洗后投递 |
| Admin Dashboard | HTTP | `POST /v1/admin/withdraw/{id}/approve` 触发提现审批 |

### 下游（输出）

| 去向 | 协议 | 说明 |
|------|------|------|
| Admin Dashboard | HTTP | 查询结算报表、交易流水 |
| ADX Core (Pacing) | HTTP | `GET /v1/pacing/{campaign_id}` 获取节奏因子 |

### 基础设施

| 组件 | 用途 |
|------|------|
| PostgreSQL | 账户、交易、结算单持久化存储 |
| Redis | 广告主余额原子扣费、节奏因子缓存 |
| Kafka | 消费广告事件（Consumer Group: `adortb-billing`） |
| HashiCorp Vault | 敏感配置（DB 密码、Redis 密码）动态读取 |
| Jaeger (OTLP) | 分布式链路追踪，端口 4318 |
| Prometheus | 指标采集，`/metrics` 暴露 |

## 深入阅读

- [docs/architecture.md](docs/architecture.md) — 内部架构图、数据流图、关键时序图
- [migrations/001_billing.up.sql](migrations/001_billing.up.sql) — 核心表结构
- [migrations/002_multitenant.up.sql](migrations/002_multitenant.up.sql) — 多租户扩展
- [README.md](README.md) — 快速启动与 API 完整列表
