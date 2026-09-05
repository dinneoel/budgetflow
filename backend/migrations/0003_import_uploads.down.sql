DROP TABLE import_upload_data;

DELETE FROM import_batches WHERE account_id IS NULL;
ALTER TABLE import_batches ALTER COLUMN account_id SET NOT NULL;
