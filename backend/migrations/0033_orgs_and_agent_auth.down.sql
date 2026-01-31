-- 0033_orgs_and_agent_auth.down.sql
DROP TABLE IF EXISTS node_registration_tokens;
DROP TABLE IF EXISTS agent_credentials;
DROP INDEX IF EXISTS idx_nodes_org;
ALTER TABLE nodes DROP COLUMN IF EXISTS org_id;
DROP TABLE IF EXISTS organization_members;
DROP TABLE IF EXISTS organizations;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_type WHERE typname = 'org_role') THEN
        DROP TYPE org_role;
    END IF;
END $$;
