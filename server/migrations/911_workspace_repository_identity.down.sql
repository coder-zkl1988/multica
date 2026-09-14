UPDATE workspace AS w
SET repos = COALESCE((
    SELECT jsonb_agg(item.value - 'id' ORDER BY item.ordinality)
    FROM jsonb_array_elements(w.repos) WITH ORDINALITY AS item(value, ordinality)
), '[]'::jsonb)
WHERE jsonb_typeof(w.repos) = 'array';
