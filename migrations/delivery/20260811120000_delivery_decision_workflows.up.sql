-- Phase C authority spine: immutable decisions, selections, and compiled
-- workflows. No table contains executable credentials or a remote-write
-- runtime flag; compiled remote-write steps are audit-only and blocked in Go.
CREATE TABLE delivery_decisions (
  organization_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  project_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  decision_id VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  plan_id VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  plan_version INT NOT NULL,
  schema_version VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  policy_version VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  canonical_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  decision_json JSON NOT NULL,
  created_by VARCHAR(128) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  PRIMARY KEY (organization_id, project_id, decision_id),
  UNIQUE KEY uq_delivery_decision_input (organization_id, project_id, canonical_hash),
  KEY idx_delivery_decisions_plan (organization_id, project_id, plan_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE delivery_compiled_workflows (
  organization_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  project_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  workflow_id VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  decision_id VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  decision_canonical_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  configuration_canonical_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  schema_version VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  compiler_version VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  status VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  remote_write_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  canonical_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  workflow_json JSON NOT NULL,
  created_by VARCHAR(128) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  PRIMARY KEY (organization_id, project_id, workflow_id),
  UNIQUE KEY uq_delivery_workflow_hash (organization_id, project_id, canonical_hash),
  CONSTRAINT fk_delivery_workflow_decision FOREIGN KEY (organization_id, project_id, decision_id)
    REFERENCES delivery_decisions (organization_id, project_id, decision_id),
  CONSTRAINT chk_delivery_workflow_no_remote_write CHECK (remote_write_enabled = FALSE),
  CONSTRAINT chk_delivery_workflow_ready CHECK (status = 'ready_for_final_approval')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE delivery_decision_selections (
  organization_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  project_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  selection_id VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  decision_id VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  decision_canonical_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  candidate_id VARCHAR(160) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  configuration_id VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  configuration_version INT NOT NULL,
  configuration_canonical_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  workflow_id VARCHAR(96) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  workflow_canonical_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  selection_json JSON NOT NULL,
  idempotency_key VARCHAR(160) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  request_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
  created_by VARCHAR(128) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  PRIMARY KEY (organization_id, project_id, selection_id),
  UNIQUE KEY uq_delivery_decision_selection (organization_id, project_id, decision_id),
  UNIQUE KEY uq_delivery_decision_selection_idempotency (organization_id, project_id, idempotency_key),
  CONSTRAINT fk_delivery_selection_decision FOREIGN KEY (organization_id, project_id, decision_id)
    REFERENCES delivery_decisions (organization_id, project_id, decision_id),
  CONSTRAINT fk_delivery_selection_configuration FOREIGN KEY (organization_id, project_id, configuration_id, configuration_version)
    REFERENCES delivery_platform_configurations (organization_id, project_id, configuration_id, version_number),
  CONSTRAINT fk_delivery_selection_workflow FOREIGN KEY (organization_id, project_id, workflow_id)
    REFERENCES delivery_compiled_workflows (organization_id, project_id, workflow_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
