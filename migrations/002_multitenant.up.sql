-- 多租户支持：为计费表添加 tenant_id 列

ALTER TABLE advertiser_accounts ADD COLUMN IF NOT EXISTS tenant_id BIGINT NOT NULL DEFAULT 1;
ALTER TABLE publisher_accounts  ADD COLUMN IF NOT EXISTS tenant_id BIGINT NOT NULL DEFAULT 1;

CREATE INDEX IF NOT EXISTS idx_advertiser_accounts_tenant ON advertiser_accounts(tenant_id);
CREATE INDEX IF NOT EXISTS idx_publisher_accounts_tenant  ON publisher_accounts(tenant_id);
