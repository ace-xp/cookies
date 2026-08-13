-- 米云素材一直挂在 source_kind='external' 下面，但那个标签在界面上写的是
-- 「外部引用」，指的是平台外的竞品参照证据——那些永远不能拿去投放。米云的素材有
-- platform_asset_id、能投、要跑归因，和竞品证据是两码事。同一个词指两样东西，
-- 看的人一定会搞错。
ALTER TABLE insight_assets
  DROP CONSTRAINT chk_insight_assets_source_kind;

ALTER TABLE insight_assets
  ADD CONSTRAINT chk_insight_assets_source_kind
  CHECK (source_kind IN ('creative', 'upload', 'external', 'miyun'));

-- 存量按「谁指着它」认，不按 source_ref 的前缀认：米云那三条入库路径写出来的
-- source_ref 长得都不一样（采集是 miyun://material/… 或核实过的原始链接，
-- 回流是 miyun_handoff_return:…，手工导入是人自己填的），靠前缀一定漏。
-- 而这两张米云表的 insight_asset_id 是准的：只有米云那边会往里写。
UPDATE insight_assets a
  JOIN insight_miyun_materials m
    ON m.organization_id = a.organization_id AND m.insight_asset_id = a.id
   SET a.source_kind = 'miyun'
 WHERE a.source_kind = 'external';

UPDATE insight_assets a
  JOIN insight_miyun_handoff_returns r
    ON r.organization_id = a.organization_id AND r.insight_asset_id = a.id
   SET a.source_kind = 'miyun'
 WHERE a.source_kind = 'external';
