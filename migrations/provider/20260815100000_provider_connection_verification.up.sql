-- The Settings page shows when a video connection was last verified and what
-- the upstream said. Persisting it here keeps the answer after a page reload.
ALTER TABLE provider_connections
  ADD COLUMN last_verified_at DATETIME(6) NULL AFTER version,
  ADD COLUMN last_verification_ok TINYINT(1) NULL AFTER last_verified_at,
  ADD COLUMN last_verification_message VARCHAR(512) NULL AFTER last_verification_ok;
