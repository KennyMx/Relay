-- Classifier metadata only; no prompt, response text, or credential is stored.
ALTER TABLE requests ADD COLUMN routing jsonb;
