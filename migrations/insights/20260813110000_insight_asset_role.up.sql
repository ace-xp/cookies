-- 素材有两种身份：台账（平台里所有素材的账本）和分析对象（真正投流、要跑归因的成品）。
-- 身份和分析进度是两个正交维度，所以不加第九个 analysis_status，而是加一列 role。
-- 存量全部是分析对象，回填 'analysis'，现状行为一个字节都不变。
ALTER TABLE insight_assets
  ADD COLUMN role VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'analysis' AFTER project_id;

ALTER TABLE insight_assets
  ADD CONSTRAINT chk_insight_assets_role CHECK (role IN ('ledger', 'analysis'));

-- 台账靠后台自动收录，同一个平台素材版本可能被收两次（重试、回填）。
-- MySQL 的唯一键不约束含 NULL 的行，所以用生成列把「台账 + 有平台引用」这一种情况
-- 挑出来做唯一键：手工登记的分析素材（role='analysis'）和没有平台引用的行都算 NULL，
-- 不受约束——它们本来就允许一个平台素材登记多条。
ALTER TABLE insight_assets
  ADD COLUMN ledger_object_key VARCHAR(160) CHARACTER SET ascii COLLATE ascii_bin
    GENERATED ALWAYS AS (
      CASE WHEN role = 'ledger' AND platform_asset_id IS NOT NULL
           THEN CONCAT(platform_asset_id, ':', platform_asset_version)
           ELSE NULL END
    ) STORED;

ALTER TABLE insight_assets
  ADD UNIQUE KEY uq_insight_assets_ledger_object (organization_id, ledger_object_key);

-- 台账清单按 (role, updated_at, id) 游标翻页，四个分析队列按 role 过滤后再看状态。
ALTER TABLE insight_assets
  ADD KEY idx_insight_assets_role (organization_id, project_id, role, updated_at, id);
