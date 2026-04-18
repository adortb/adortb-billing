# adortb-billing — 架构文档

## 1. 内部架构图

```
┌──────────────────────────────────────────────────────────────────┐
│                        adortb-billing (:8085)                    │
│                                                                  │
│  ┌──────────────┐    ┌────────────────────────────────────────┐ │
│  │ Kafka        │    │  HTTP API Layer (internal/api)         │ │
│  │ Consumer     │    │  ┌──────────────┬────────────────────┐ │ │
│  │ (event.go)   │    │  │ advertiser   │ publisher          │ │ │
│  └──────┬───────┘    │  │ /recharge    │ /account           │ │ │
│         │            │  │ /account     │ /settlements       │ │ │
│         ▼            │  │ /transactions│ /withdraw          │ │ │
│  ┌──────────────────┐│  ├──────────────┼────────────────────┤ │ │
│  │ Service Layer    ││  │ platform     │ pacing             │ │ │
│  │                  ││  │ /daily       │ /{campaign_id}     │ │ │
│  │ AdvertiserBilling││  │              │ /recalc            │ │ │
│  │   Service        ││  ├──────────────┴────────────────────┤ │ │
│  │                  ││  │ admin /withdraw/{id}/approve       │ │ │
│  │ PublisherBilling ││  └────────────────────────────────────┘ │ │
│  │   Service        ││                                         │ │
│  │                  ││  ┌───────────────┐  ┌────────────────┐ │ │
│  │ Platform         ││  │ Pacing        │  │ Metrics        │ │ │
│  │   Service        ││  │ Tracker       │  │ (Prometheus)   │ │ │
│  │                  ││  │ (32 shards)   │  │                │ │ │
│  │ Settlement       ││  └───────────────┘  └────────────────┘ │ │
│  │   Job (cron)     ││                                         │ │
│  └─────────┬────────┘│  ┌────────────────────────────────────┐ │ │
│            │         │  │ Tracing (OpenTelemetry/Jaeger)      │ │ │
│            ▼         │  └────────────────────────────────────┘ │ │
│  ┌──────────────────┐└────────────────────────────────────────┘ │
│  │ Repo Layer       │                                           │
│  │ (internal/repo)  │                                           │
│  │  advertiser_repo │                                           │
│  │  publisher_repo  │                                           │
│  │  transaction_repo│                                           │
│  └──────┬───────────┘                                           │
└─────────┼────────────────────────────────────────────────────────┘
          │
          ▼
┌─────────────────────────────────────┐
│           基础设施层                  │
│  ┌──────────┐  ┌──────┐  ┌───────┐ │
│  │PostgreSQL│  │Redis │  │ Vault │ │
│  └──────────┘  └──────┘  └───────┘ │
└─────────────────────────────────────┘
```

## 2. 数据流图：事件 → 扣费 → 结算

```
                        ┌─────────────────────────────────────────────┐
                        │          Kafka: adortb.events               │
                        │  {event_type, campaign_id, advertiser_id,   │
                        │   publisher_id, tenant_id, amount, ts}      │
                        └────────────────────┬────────────────────────┘
                                             │ Consumer Group: adortb-billing
                                             ▼
                        ┌─────────────────────────────────────────────┐
                        │         consumer/event.go                   │
                        │  1. 反序列化事件                              │
                        │  2. 路由到对应 Service                        │
                        └────────────┬────────────────────────────────┘
                                     │
              ┌──────────────────────┼──────────────────────┐
              ▼                      ▼                      ▼
   ┌─────────────────┐   ┌─────────────────────┐  ┌──────────────────┐
   │AdvertiserBilling│   │ PublisherBilling     │  │ Pacing Tracker   │
   │  扣费流程        │   │  收入记账             │  │  更新消耗曲线     │
   └────────┬────────┘   └──────────┬──────────┘  └──────────────────┘
            │                       │
            ▼                       ▼
   ┌──────────────────┐   ┌──────────────────────┐
   │ Redis Lua 原子    │   │ PostgreSQL            │
   │ DECRBY balance   │   │ INSERT revenue_pending│
   └────────┬─────────┘   └──────────────────────┘
            │ 失败/不足
            ▼
   ┌──────────────────┐          ┌──────────────────────────────────┐
   │ PG 行锁回落       │          │       每日结算任务 UTC 01:00      │
   │ SELECT FOR UPDATE│          │  settlement_job.go               │
   │ UPDATE balance   │          │                                  │
   │ INSERT tx        │          │  revenue_pending                 │
   └──────────────────┘          │      │                           │
                                 │      ▼ 批量确认                   │
                                 │  revenue_settled                 │
                                 │      │                           │
                                 │      ▼ 媒体方申请提现              │
                                 │  revenue_withdrawn               │
                                 └──────────────────────────────────┘
```

## 3. 与其他模块交互图

```
┌──────────────────┐        Kafka: adortb.events        ┌───────────────────┐
│   ADX Core       │ ──────────────────────────────────▶ │  adortb-billing   │
│  (竞价引擎)       │                                     │   (本服务 ★)      │
└──────────────────┘                                     └────────┬──────────┘
                                                                  │
┌──────────────────┐        Kafka: adortb.events                  │ HTTP GET
│  Event Pipeline  │ ──────────────────────────────────▶          │ /v1/pacing/{id}
│  (事件清洗)       │                                     ┌────────▼──────────┐
└──────────────────┘                                     │   ADX Core        │
                                                         │  (拉取节奏因子)    │
┌──────────────────┐        HTTP GET                     └───────────────────┘
│  Admin Dashboard │ ◀────────────────────────────────── │  adortb-billing   │
│  (管理后台)       │  /v1/platform/daily                 │                   │
│                  │  /v1/publisher/{id}/settlements      │                   │
│                  │  /v1/advertiser/{id}/transactions    │                   │
│                  │                                     │                   │
│                  │ ──── HTTP POST ────────────────────▶ │                   │
│                  │  /v1/admin/withdraw/{id}/approve     │                   │
└──────────────────┘                                     └───────────────────┘

基础设施交互：
┌───────────────────────────────────────────────────────────────────────┐
│  adortb-billing                                                       │
│    │── READ/WRITE ──▶ PostgreSQL  (账户余额、交易流水、结算单)           │
│    │── DECRBY/GET ──▶ Redis       (广告主余额热缓存、节奏因子缓存)       │
│    │── CONSUME ─────▶ Kafka       (adortb.events, consumer group)     │
│    │── GET SECRET ──▶ Vault       (DB/Redis 密码动态读取)              │
│    │── EXPORT SPAN ─▶ Jaeger      (OTLP HTTP :4318)                   │
│    └── EXPOSE ──────▶ Prometheus  (/metrics :8085)                    │
└───────────────────────────────────────────────────────────────────────┘
```

## 4. 关键流程时序图

### 4.1 广告事件计费完整流程

```
ADX Core        Kafka           Consumer         AdvertiserBilling     Redis          PostgreSQL
   │               │                │                    │               │                │
   │  publish event│                │                    │               │                │
   │──────────────▶│                │                    │               │                │
   │               │ poll           │                    │               │                │
   │               │───────────────▶│                    │               │                │
   │               │                │ DeductBalance(req) │               │                │
   │               │                │───────────────────▶│               │                │
   │               │                │                    │ Lua DECRBY    │                │
   │               │                │                    │──────────────▶│                │
   │               │                │                    │  ◀── ok/err ──│                │
   │               │                │                    │               │                │
   │               │                │     [Redis 成功]    │               │                │
   │               │                │                    │  INSERT tx    │                │
   │               │                │                    │───────────────────────────────▶│
   │               │                │                    │  ◀────────────────────── ok ───│
   │               │                │                    │               │                │
   │               │                │     [Redis 失败，回落 PG]           │                │
   │               │                │                    │  BEGIN        │                │
   │               │                │                    │───────────────────────────────▶│
   │               │                │                    │  SELECT FOR UPDATE             │
   │               │                │                    │───────────────────────────────▶│
   │               │                │                    │  ◀───────── account row ───────│
   │               │                │                    │  [check balance >= amount]     │
   │               │                │                    │  UPDATE balance                │
   │               │                │                    │───────────────────────────────▶│
   │               │                │                    │  INSERT transaction            │
   │               │                │                    │───────────────────────────────▶│
   │               │                │                    │  COMMIT                        │
   │               │                │                    │───────────────────────────────▶│
   │               │                │  ◀────── result ───│               │                │
   │               │                │ commit offset      │               │                │
   │               │◀───────────────│                    │               │                │
```

### 4.2 媒体方提现审批流程

```
Publisher      API Handler      PublisherBilling      PostgreSQL       Admin
    │               │                 │                   │               │
    │ POST /withdraw│                 │                   │               │
    │──────────────▶│                 │                   │               │
    │               │ RequestWithdraw │                   │               │
    │               │────────────────▶│                   │               │
    │               │                 │ BEGIN             │               │
    │               │                 │──────────────────▶│               │
    │               │                 │ SELECT settled    │               │
    │               │                 │──────────────────▶│               │
    │               │                 │ INSERT withdrawal (pending)       │
    │               │                 │──────────────────▶│               │
    │               │                 │ COMMIT            │               │
    │               │                 │──────────────────▶│               │
    │               │ ◀── withdraw_id─│                   │               │
    │ ◀─── 202 ─────│                 │                   │               │
    │               │                 │                   │               │
    │               │                 │                   │  POST /approve│
    │               │                 │                   │◀──────────────│
    │               │                 │                   │               │
    │               │                 │ BEGIN             │               │
    │               │                 │──────────────────▶│               │
    │               │                 │ UPDATE pending→approved           │
    │               │                 │──────────────────▶│               │
    │               │                 │ UPDATE revenue_withdrawn          │
    │               │                 │──────────────────▶│               │
    │               │                 │ COMMIT            │               │
    │               │                 │──────────────────▶│               │
    │               │                 │                   │ ◀── 200 ──────│
```

### 4.3 预算节奏因子计算流程

```
ADX Core        API Handler      PacingTracker         Redis
    │                │                  │                 │
    │ GET /pacing/42 │                  │                 │
    │───────────────▶│                  │                 │
    │                │ GetFactor(42)    │                 │
    │                │─────────────────▶│                 │
    │                │                  │ GET pacing:42   │
    │                │                  │────────────────▶│
    │                │                  │ ◀─── factor ────│ (cache hit)
    │                │ ◀─── factor ─────│                 │
    │ ◀── {factor} ──│                  │                 │
    │                │                  │                 │
    │                │                  │  [cache miss / POST /recalc]
    │                │                  │                 │
    │                │ Recalc(42)       │                 │
    │                │─────────────────▶│                 │
    │                │                  │ 选取分片锁        │
    │                │                  │ (42 % 32 = 10)  │
    │                │                  │                 │
    │                │                  │ 读取今日消耗曲线  │
    │                │                  │────────────────▶│
    │                │                  │ 计算新节奏因子    │
    │                │                  │ SET pacing:42   │
    │                │                  │ (TTL 60s)       │
    │                │                  │────────────────▶│
    │                │ ◀── new factor ──│                 │
    │ ◀── {factor} ──│                  │                 │
```

### 4.4 每日结算任务流程

```
Cron (UTC 01:00)    SettlementJob         PostgreSQL
        │                  │                   │
        │ Trigger           │                   │
        │──────────────────▶│                   │
        │                   │ BEGIN             │
        │                   │──────────────────▶│
        │                   │ SELECT publishers │
        │                   │  WHERE pending > 0│
        │                   │──────────────────▶│
        │                   │ ◀── publisher list│
        │                   │                   │
        │                   │  [for each publisher]
        │                   │                   │
        │                   │ INSERT settlement_record
        │                   │  (amount=pending, date=today)
        │                   │──────────────────▶│
        │                   │ UPDATE publisher  │
        │                   │  SET settled += pending
        │                   │      pending = 0  │
        │                   │──────────────────▶│
        │                   │                   │
        │                   │ COMMIT            │
        │                   │──────────────────▶│
        │                   │                   │
        │ ◀── done ─────────│                   │
```
