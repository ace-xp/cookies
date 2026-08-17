ALTER TABLE provider_connections
  DROP COLUMN last_verification_message,
  DROP COLUMN last_verification_ok,
  DROP COLUMN last_verified_at;
