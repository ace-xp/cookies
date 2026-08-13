-- Forward-only compatibility for an early draft that made canonical business
-- hashes globally unique. Immutable identity is (id, version); equal canonical
-- payloads under distinct identities are valid and are checked byte-for-byte
-- by the repository.
SET @drop_delivery_intent_hash_unique = IF(
  EXISTS(SELECT 1 FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'delivery_intents' AND index_name = 'uq_delivery_intents_hash'),
  'ALTER TABLE delivery_intents DROP INDEX uq_delivery_intents_hash',
  'SELECT 1'
);
PREPARE drop_delivery_intent_hash_unique_stmt FROM @drop_delivery_intent_hash_unique;
EXECUTE drop_delivery_intent_hash_unique_stmt;
DEALLOCATE PREPARE drop_delivery_intent_hash_unique_stmt;

SET @drop_delivery_configuration_hash_unique = IF(
  EXISTS(SELECT 1 FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'delivery_platform_configurations' AND index_name = 'uq_delivery_platform_configurations_hash'),
  'ALTER TABLE delivery_platform_configurations DROP INDEX uq_delivery_platform_configurations_hash',
  'SELECT 1'
);
PREPARE drop_delivery_configuration_hash_unique_stmt FROM @drop_delivery_configuration_hash_unique;
EXECUTE drop_delivery_configuration_hash_unique_stmt;
DEALLOCATE PREPARE drop_delivery_configuration_hash_unique_stmt;
