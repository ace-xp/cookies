ALTER TABLE provider_connection_revisions
  DROP COLUMN created_by;

ALTER TABLE provider_connections
  DROP COLUMN last_verification_outcome;

ALTER TABLE provider_connections
  DROP CHECK chk_provider_connection_type,
  ADD CONSTRAINT chk_provider_connection_type
    CHECK (connection_type IN ('adapter_gateway', 'ark', 'minimax_speech', 'las_operator'));
