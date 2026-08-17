-- 回退把米云并回外部引用。并回去是有损的：并之前哪些是米云，回退之后就分不出来了。
UPDATE insight_assets SET source_kind = 'external' WHERE source_kind = 'miyun';

ALTER TABLE insight_assets
  DROP CONSTRAINT chk_insight_assets_source_kind;

ALTER TABLE insight_assets
  ADD CONSTRAINT chk_insight_assets_source_kind
  CHECK (source_kind IN ('creative', 'upload', 'external'));
