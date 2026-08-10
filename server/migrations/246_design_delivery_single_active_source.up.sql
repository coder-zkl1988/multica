WITH ranked_active AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY workspace_id, source_issue_id
               ORDER BY delivered_at DESC, updated_at DESC, id DESC
           ) AS active_rank
    FROM design_delivery
    WHERE status = 'active'
)
UPDATE design_delivery
SET status = 'superseded',
    updated_at = now()
WHERE id IN (
    SELECT id FROM ranked_active WHERE active_rank > 1
);

DROP INDEX IF EXISTS idx_design_delivery_active_pair;

CREATE UNIQUE INDEX idx_design_delivery_active_source
    ON design_delivery(workspace_id, source_issue_id)
    WHERE status = 'active';
