-- Forward-only correction for databases that applied an early draft of the
-- platform-configuration migration with a 32-character hash-algorithm field.
-- The frozen algorithm identifier is 37 ASCII characters.
ALTER TABLE delivery_intents
  MODIFY hash_algorithm VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL;

ALTER TABLE delivery_platform_configurations
  MODIFY hash_algorithm VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL;
