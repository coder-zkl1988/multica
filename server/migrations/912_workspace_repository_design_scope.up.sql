-- Design Center repository scope is owned by Settings > Repositories, not by
-- project_resource. The two identities intentionally coexist and neither is a
-- foreign key; repository relationships are validated in application code.
ALTER TABLE project_design_system ADD COLUMN workspace_repository_id UUID;
ALTER TABLE design_file ADD COLUMN workspace_repository_id UUID;
ALTER TABLE design_document ADD COLUMN workspace_repository_id UUID;
ALTER TABLE design_document ALTER COLUMN project_id DROP NOT NULL;

-- Preserve existing repository-scoped Design Center content when its project
-- repository URL already exists in Settings. Project associations remain
-- untouched; these CTEs only add the workspace-repository projection.
WITH candidates AS (
    SELECT system.id AS target_id,
           (entry.value->>'id')::uuid AS repository_id,
           row_number() OVER (PARTITION BY system.id ORDER BY entry.ordinality) AS priority
    FROM project_design_system AS system
    JOIN project_resource AS resource
      ON resource.id = system.project_resource_id
     AND resource.workspace_id = system.workspace_id
    JOIN workspace AS w ON w.id = system.workspace_id
    CROSS JOIN LATERAL jsonb_array_elements(w.repos) WITH ORDINALITY AS entry(value, ordinality)
    WHERE (entry.value->>'id') ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
      AND btrim(entry.value->>'url') = btrim(resource.resource_ref->>'url')
), matches AS (
    SELECT target_id, repository_id FROM candidates WHERE priority = 1
)
UPDATE project_design_system AS system
SET workspace_repository_id = matches.repository_id
FROM matches
WHERE system.id = matches.target_id;

WITH candidates AS (
    SELECT file.id AS target_id,
           (entry.value->>'id')::uuid AS repository_id,
           row_number() OVER (PARTITION BY file.id ORDER BY entry.ordinality) AS priority
    FROM design_file AS file
    JOIN project_resource AS resource
      ON resource.id = file.project_resource_id
     AND resource.workspace_id = file.workspace_id
    JOIN workspace AS w ON w.id = file.workspace_id
    CROSS JOIN LATERAL jsonb_array_elements(w.repos) WITH ORDINALITY AS entry(value, ordinality)
    WHERE (entry.value->>'id') ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
      AND btrim(entry.value->>'url') = btrim(resource.resource_ref->>'url')
), matches AS (
    SELECT target_id, repository_id FROM candidates WHERE priority = 1
)
UPDATE design_file AS file
SET workspace_repository_id = matches.repository_id
FROM matches
WHERE file.id = matches.target_id;

WITH candidates AS (
    SELECT document.id AS target_id,
           (entry.value->>'id')::uuid AS repository_id,
           row_number() OVER (PARTITION BY document.id ORDER BY entry.ordinality) AS priority
    FROM design_document AS document
    JOIN project_resource AS resource
      ON resource.id = document.project_resource_id
     AND resource.workspace_id = document.workspace_id
    JOIN workspace AS w ON w.id = document.workspace_id
    CROSS JOIN LATERAL jsonb_array_elements(w.repos) WITH ORDINALITY AS entry(value, ordinality)
    WHERE (entry.value->>'id') ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
      AND btrim(entry.value->>'url') = btrim(resource.resource_ref->>'url')
), matches AS (
    SELECT target_id, repository_id FROM candidates WHERE priority = 1
)
UPDATE design_document AS document
SET workspace_repository_id = matches.repository_id
FROM matches
WHERE document.id = matches.target_id;
