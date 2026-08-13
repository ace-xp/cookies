ALTER TABLE insight_assets DROP KEY idx_insight_assets_role;
ALTER TABLE insight_assets DROP KEY uq_insight_assets_ledger_object;
ALTER TABLE insight_assets DROP COLUMN ledger_object_key;
ALTER TABLE insight_assets DROP CONSTRAINT chk_insight_assets_role;
ALTER TABLE insight_assets DROP COLUMN role;
