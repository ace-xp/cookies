-- 特征来源新增 derived：客观可测层。
--
-- Go 那边从一开始就有三层（assets.go 的 SourceAI / SourceHuman / SourceDerived），
-- 归因准入 AdmissibleForAttribution() 认 derived 和 human，相似度打分也把 derived
-- 算成硬重叠。但库里的 CHECK 只放 ('ai','human') 过——也就是说这一层从建表那天起
-- 就写不进去，一条都没有。素材的时长、画幅这些明明能量出来的东西，只能靠人写一句
-- 画面描述让模型猜，然后落成 ai 层，再被归因挡在外面。
--
-- 这一条放开的是写入口。derived 的定义是「从文件本身量出来的」：ffprobe 读到的
-- 时长、宽高，不是模型的推断，也不是人的判断，所以它既不该带机器置信度，也不该
-- 进人工复核队列。
--
-- MySQL 改不了已有的 CHECK，只能先删后建。约束名与
-- 20260729103000_insight_asset_index.up.sql 里的定义一致。

ALTER TABLE insight_asset_features
  DROP CHECK chk_insight_asset_features_source;

ALTER TABLE insight_asset_features
  ADD CONSTRAINT chk_insight_asset_features_source
  CHECK (source IN ('ai', 'human', 'derived'));

-- 客观可测层不带置信度。量出来的数没有「可能是 30 秒」这回事——真量不到就别写，
-- 写了就是确定的。不设这一条的话，探测失败时补一个 confidence='low' 混过去会变成
-- 一种很自然的写法，而那正好把这一层的意义抹掉：归因认 derived，靠的就是它不猜。
ALTER TABLE insight_asset_features
  ADD CONSTRAINT chk_insight_asset_features_derived_confidence
  CHECK (source <> 'derived' OR confidence = '');
