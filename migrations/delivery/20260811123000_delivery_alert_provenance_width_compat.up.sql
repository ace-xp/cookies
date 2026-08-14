-- Forward-only compatibility for environments that applied an early v2
-- configuration migration before the complete simulator provenance identifier
-- was widened. No alert payload or identity is rewritten.
ALTER TABLE delivery_alerts
  MODIFY dataset_version VARCHAR(160) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  MODIFY fixture_version VARCHAR(160) CHARACTER SET ascii COLLATE ascii_bin NOT NULL;
