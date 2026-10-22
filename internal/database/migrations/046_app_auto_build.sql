-- Go sites can turn off the build+restart that runs after every git deploy
-- and build manually from the Deployment tab instead. Existing sites keep
-- the previous behaviour (auto build on).
ALTER TABLE websites ADD COLUMN app_auto_build INTEGER NOT NULL DEFAULT 1;
