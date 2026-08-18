ALTER TABLE avatars ADD COLUMN IF NOT EXISTS idempotency_key VARCHAR(255);

CREATE UNIQUE INDEX IF NOT EXISTS uq_avatars_idempotency_key ON avatars(idempotency_key) WHERE idempotency_key IS NOT NULL;
