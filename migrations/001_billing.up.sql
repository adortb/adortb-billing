-- 广告主账户余额
CREATE TABLE IF NOT EXISTS advertiser_accounts (
    advertiser_id BIGINT PRIMARY KEY,
    balance       DECIMAL(15,4) NOT NULL DEFAULT 0,
    frozen        DECIMAL(15,4) NOT NULL DEFAULT 0,
    total_recharge DECIMAL(15,4) NOT NULL DEFAULT 0,
    total_spent   DECIMAL(15,4) NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    updated_at    TIMESTAMPTZ DEFAULT NOW()
);

-- 媒体方账户
CREATE TABLE IF NOT EXISTS publisher_accounts (
    publisher_id       BIGINT PRIMARY KEY,
    revenue_pending    DECIMAL(15,4) NOT NULL DEFAULT 0,
    revenue_settled    DECIMAL(15,4) NOT NULL DEFAULT 0,
    revenue_withdrawn  DECIMAL(15,4) NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ DEFAULT NOW(),
    updated_at         TIMESTAMPTZ DEFAULT NOW()
);

-- 流水记录
CREATE TABLE IF NOT EXISTS balance_transactions (
    id           BIGSERIAL PRIMARY KEY,
    account_type VARCHAR(20)    NOT NULL,
    account_id   BIGINT         NOT NULL,
    tx_type      VARCHAR(30)    NOT NULL,
    amount       DECIMAL(15,4)  NOT NULL,
    balance_after DECIMAL(15,4) NOT NULL,
    ref_type     VARCHAR(30),
    ref_id       VARCHAR(64),
    description  TEXT,
    created_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_bt_account ON balance_transactions(account_type, account_id, created_at DESC);

-- 每日结算汇总
CREATE TABLE IF NOT EXISTS daily_settlements (
    id            BIGSERIAL PRIMARY KEY,
    date          DATE          NOT NULL,
    advertiser_id BIGINT,
    publisher_id  BIGINT,
    campaign_id   BIGINT,
    impressions   BIGINT        NOT NULL DEFAULT 0,
    clicks        BIGINT        NOT NULL DEFAULT 0,
    gross_revenue DECIMAL(15,4) NOT NULL DEFAULT 0,
    platform_fee  DECIMAL(15,4) NOT NULL DEFAULT 0,
    net_revenue   DECIMAL(15,4) NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE (date, advertiser_id, publisher_id, campaign_id)
);

-- 提现请求
CREATE TABLE IF NOT EXISTS withdraw_requests (
    id           BIGSERIAL PRIMARY KEY,
    publisher_id BIGINT         NOT NULL,
    amount       DECIMAL(15,4)  NOT NULL,
    status       VARCHAR(20)    NOT NULL DEFAULT 'pending',
    bank_info    JSONB,
    reviewed_by  BIGINT,
    reviewed_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ DEFAULT NOW()
);

-- 平台配置
CREATE TABLE IF NOT EXISTS platform_configs (
    key        VARCHAR(64) PRIMARY KEY,
    value      TEXT        NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

INSERT INTO platform_configs (key, value)
VALUES ('platform_fee_rate', '0.10')
ON CONFLICT DO NOTHING;
