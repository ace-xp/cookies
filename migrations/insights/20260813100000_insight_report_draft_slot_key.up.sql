-- (项目 + 窗口) 的唯一键只该管草稿，不该把已确认的报告也算进去。
--
-- 现在的键是 uq_insight_reports_project_execution_window
-- (organization_id, project_id, execution_id, window_start, window_end)。
-- 它挡住的本来是「同一个窗口开出两份没提交的草稿」，但已确认的报告也占着这个位置，
-- 于是出现这条死路：
--
--   1. 人在分析页对 7 月这个窗口记了几笔，提交了复盘，提交时没选投放执行
--      （执行本来就是选填的），这份已确认报告的 execution_id 是空串；
--   2. 隔两天他在同一个窗口又看到一条值得留的结论，按「记一笔」；
--   3. PinFinding 先找草稿——已确认的不算草稿，找不到，于是去建一份新草稿，
--      键还是 (org, project, '', 7-01, 7-31)，正好撞上那份已确认报告；
--   4. 撞键被当成「草稿被另一次记一笔抢先建了」，重试三次，三次都撞，
--      最后抛出版本冲突。人看到的是「有人同时改了，请刷新重试」——
--      而刷新多少次都没用，这个窗口从此再也记不了一笔。
--
-- PRD §15.3 要的恰恰是这一步能走通：复盘提交之后还想改，就开下一份草稿，
-- 老的那份定格留档。所以确认过的报告不能再占唯一键的位置。
--
-- 做法是给唯一键加一列 draft_slot：草稿是 1，已确认的是 NULL。MySQL 的唯一键
-- 不约束含 NULL 的行，于是同一个 (项目 + 执行 + 窗口) 下可以躺着任意多份已确认
-- 的历史报告，但同时只能有一份没提交的草稿——这正是原来想表达的意思。
--
-- 不改成「删掉唯一键、由应用层保证」：那样两次并发的记一笔会各建一份草稿，
-- 第二笔记进哪一份取决于时序，人会看到自己记的东西时有时无。
ALTER TABLE insight_reports
  ADD COLUMN draft_slot TINYINT GENERATED ALWAYS AS (IF(status = 'draft', 1, NULL)) STORED;

ALTER TABLE insight_reports
  DROP INDEX uq_insight_reports_project_execution_window,
  ADD UNIQUE KEY uq_insight_reports_open_draft
    (organization_id, project_id, execution_id, window_start, window_end, draft_slot);
