ALTER TABLE delivery_observatory_feedback
  ADD COLUMN run_outcome VARCHAR(40) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER run_canonical_hash;

UPDATE delivery_observatory_feedback AS feedback
JOIN delivery_observatory_runs AS observatory_run
  ON observatory_run.organization_id = feedback.organization_id
 AND observatory_run.project_id = feedback.project_id
 AND observatory_run.run_id = feedback.run_id
SET feedback.run_outcome = observatory_run.outcome
WHERE feedback.run_outcome IS NULL;

ALTER TABLE delivery_observatory_feedback
  MODIFY COLUMN run_outcome VARCHAR(40) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  ADD CONSTRAINT chk_delivery_observatory_feedback_outcome CHECK (run_outcome IN ('in_sync','drift_detected','local_form_prepared','insufficient_data','stale_data','blocked_by_asset','platform_pending','runner_failure'));
