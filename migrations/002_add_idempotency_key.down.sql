DROP INDEX IF EXISTS uq_avatars_idempotency_key;

ALTER TABLE avatars DROP COLUMN IF EXISTS idempotency_key;
