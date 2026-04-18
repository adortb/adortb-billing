# adortb-billing

广告平台计费服务，负责广告主充值/扣费、媒体方收入核算与结算、提现审批以及预算节奏控制。

## 整体架构

```
              ┌─────────────────────────────────────────┐
              │     Web SDK / iOS / Android / CTV       │
              └────────────────┬────────────────────────┘
                               ↓
                       ┌───────────────┐
                       │   ADX Core    │◀──外部 DSP─┐
                       └───────┬───────┘            │
                   ┌───────────┼───────────┐        │
                   ↓           ↓           ↓        │
              ┌────────┐ ┌────────┐ ┌────────┐     │
              │  DSP   │ │  MMP   │ │  SSAI  │─────┘
              └───┬────┘ └───┬────┘ └────────┘
                  ↓          ↓
              ┌───────────────────────────┐
              │  Event Pipeline (Kafka)   │
              └───────┬───────────────────┘
        ┌─────────────┼─────────────┐
        ↓             ↓             ↓
  ┌─────────┐   ┌──────────┐   ┌──────────┐
  │★Billing │   │   DMP    │   │   CDP    │
  └─────────┘   └──────────┘   └──────────┘
                       ↓
                  ┌──────────┐
                  │  Admin   │◀── Frontend
                  └──────────┘
```

## 核心功能

- **广告主计费**: Redis Lua 原子扣费 + PostgreSQL 行锁双层回落机制，保证高并发下的资金安全
- **媒体方结算**: 三层账户流转 `revenue_pending → revenue_settled → revenue_withdrawn`
- **提现审批流**: 状态机管理 `pending → approved → paid`
- **预算节奏控制**: 32 分片锁 Pacing Tracker，支持流量曲线学习，动态调整投放速率
- **每日结算任务**: UTC 01:00 自动执行平台日汇总与媒体方结算
- **多租户支持**: 所有数据模型携带 `tenant_id`，逻辑隔离

## 技术栈

| 分类 | 技术 |
|------|------|
| 语言 | Go 1.25.3 |
| 数据库 | PostgreSQL (lib/pq v1.12.3) |
| 缓存/原子操作 | Redis (go-redis/v9 v9.18.0) |
| 消息队列 | Kafka (IBM/sarama v1.47.0) |
| 可观测性 | Prometheus + OpenTelemetry (Jaeger HTTP OTLP) |
| 密钥管理 | HashiCorp Vault |

## 目录结构

```
adortb-billing/
├── cmd/billing/main.go                 # 应用入口，初始化依赖并启动 HTTP Server
├── internal/
│   ├── account/model.go               # 账户数据模型（广告主/媒体方/平台）
│   ├── advertiser_billing/service.go  # 广告主充值与扣费逻辑
│   ├── publisher_billing/service.go   # 媒体方收入记账与结算逻辑
│   ├── platform/service.go            # 平台方汇总服务
│   ├── platform/settlement_job.go     # 每日结算定时任务（UTC 01:00）
│   ├── consumer/event.go              # Kafka 消费者，处理 adortb.events
│   ├── api/handler.go                 # HTTP 路由与请求处理
│   ├── metrics/metrics.go             # Prometheus 自定义指标定义
│   ├── pacing/                        # 预算节奏控制（32 分片锁 Tracker）
│   ├── repo/                          # 数据访问层（PostgreSQL 查询封装）
│   └── tracing/tracing.go             # OpenTelemetry 链路追踪初始化
├── migrations/
│   ├── 001_billing.up.sql             # 基础账户与交易表
│   └── 002_multitenant.up.sql         # 多租户 tenant_id 扩展
└── go.mod
```

## 快速开始

```bash
# 安装依赖
go mod tidy

# 构建
go build -o bin/billing ./cmd/billing

# 运行单元测试
go test ./...

# 运行（需提前配置环境变量）
./bin/billing
```

## API 说明

服务监听端口 **8085**。

### 广告主

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/advertiser/{id}/recharge` | 账户充值 |
| GET | `/v1/advertiser/{id}/account` | 查询账户余额 |
| GET | `/v1/advertiser/{id}/transactions` | 查询交易流水 |

### 媒体方

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/v1/publisher/{id}/account` | 查询收入账户 |
| GET | `/v1/publisher/{id}/settlements` | 查询结算单列表 |
| POST | `/v1/publisher/{id}/withdraw` | 发起提现申请 |

### 管理

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/admin/withdraw/{id}/approve` | 审批提现申请 |
| GET | `/v1/platform/daily` | 平台日汇总报表 |

### 节奏控制

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/v1/pacing/{campaign_id}` | 获取预算节奏因子 |
| POST | `/v1/pacing/recalc` | 触发节奏重算 |

### 基础设施

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 健康检查 |
| GET | `/metrics` | Prometheus 指标暴露 |

## 环境变量配置

| 变量 | 说明 | 示例 |
|------|------|------|
| `PG_HOST` | PostgreSQL 主机 | `localhost` |
| `PG_USER` | PostgreSQL 用户名 | `billing` |
| `PG_PASSWORD` | PostgreSQL 密码 | — |
| `PG_DBNAME` | PostgreSQL 数据库名 | `adortb_billing` |
| `REDIS_ADDR` | Redis 地址 | `localhost:6379` |
| `REDIS_PASSWORD` | Redis 密码 | — |
| `KAFKA_BROKERS` | Kafka Broker 列表（逗号分隔） | `kafka:9092` |
| `VAULT_ADDR` | HashiCorp Vault 地址 | `http://vault:8200` |
| `VAULT_TOKEN` | Vault 访问令牌 | — |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Jaeger OTLP HTTP 端点 | `http://jaeger:4318` |
| `DEPLOYMENT_ENV` | 部署环境标识 | `production` |

## 服务依赖关系

```
上游（数据来源）
  ADX Core / Event Pipeline  →  Kafka topic: adortb.events  →  adortb-billing

下游（数据消费）
  adortb-billing  →  Admin Dashboard（查询结算数据）

基础设施依赖
  PostgreSQL   — 持久化账户、交易、结算记录
  Redis        — 广告主余额原子扣费缓存、节奏因子缓存
  Kafka        — 消费广告曝光/点击事件（Consumer Group: adortb-billing）
  HashiCorp Vault — 数据库密码、Redis 密码等敏感配置
  Jaeger       — 分布式链路追踪
  Prometheus   — 指标采集与监控告警
```

## 相关项目

- [adortb-core](../adortb-core) — ADX 核心竞价引擎
- [adortb-dsp](../adortb-dsp) — DSP 需求方平台
- [adortb-mmp](../adortb-mmp) — MMP 归因追踪
- [adortb-admin](../adortb-admin) — 管理后台（消费本服务结算数据）

详细架构说明请参阅 [docs/architecture.md](docs/architecture.md)。
