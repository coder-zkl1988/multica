-- Settings repositories become stable workspace-owned identities for Design
-- Center repository view. Existing JSON entries keep their URL/description and
-- receive an internal UUID; project_resource remains an independent feature.
UPDATE workspace AS w
SET repos = COALESCE((
    SELECT jsonb_agg(
        CASE
            WHEN jsonb_typeof(item.value) = 'object'
                 AND COALESCE(btrim(item.value->>'id'), '') = ''
            THEN item.value || jsonb_build_object('id', gen_random_uuid()::text)
            ELSE item.value
        END
        ORDER BY item.ordinality
    )
    FROM jsonb_array_elements(w.repos) WITH ORDINALITY AS item(value, ordinality)
), '[]'::jsonb)
WHERE jsonb_typeof(w.repos) = 'array';
