ALTER TABLE ssl_certificates ADD COLUMN dns_provider TEXT NOT NULL DEFAULT '';
ALTER TABLE ssl_certificates ADD COLUMN dns_credential TEXT NOT NULL DEFAULT '';
