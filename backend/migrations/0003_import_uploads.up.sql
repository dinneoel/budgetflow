-- The import flow picks the target account at column-mapping time, after the
-- pending batch already exists, so the account is unknown at upload.
ALTER TABLE import_batches ALTER COLUMN account_id DROP NOT NULL;

-- Parsed CSV contents and the submitted column mapping for a pending batch.
-- Removed on commit; the committed transactions are the durable record.
CREATE TABLE import_upload_data (
    batch_id   uuid PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    header     jsonb NOT NULL,
    has_header boolean NOT NULL DEFAULT true,
    rows       jsonb NOT NULL,
    mapping    jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (batch_id, user_id) REFERENCES import_batches (id, user_id) ON DELETE CASCADE
);
