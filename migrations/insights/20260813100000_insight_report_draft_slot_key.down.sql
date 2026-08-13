-- 回退前提：同一个 (项目 + 执行 + 窗口) 下如果已经躺着不止一份已确认报告，
-- 恢复旧键会失败。那是这次变更之后才可能出现的数据，回退时得先人工处理。
ALTER TABLE insight_reports
  DROP INDEX uq_insight_reports_open_draft,
  ADD UNIQUE KEY uq_insight_reports_project_execution_window
    (organization_id, project_id, execution_id, window_start, window_end);

ALTER TABLE insight_reports
  DROP COLUMN draft_slot;
