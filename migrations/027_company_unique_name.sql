-- Add unique constraint on companies.name so that self-service registration
-- cannot create two tenants with the same company name.

DO $$ BEGIN
    ALTER TABLE companies ADD CONSTRAINT companies_name_unique UNIQUE (name);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
