-- Test points: the verifiable conditions one requirement is accepted on.
--
-- Coverage used to be "how many cases are linked to the issue", so an issue
-- with one happy-path case looked as tested as one whose every rule, boundary
-- and permission had a case. A test point is one condition that can be judged
-- right or wrong on its own ("an order over the balance is refused with a
-- message"); cases declare which points they verify, and an issue is verified
-- only when every active point is covered by an active case that passes.
--
-- `number` is TP-<n> inside its issue. `origin` keeps a human-written point
-- from being rewritten by a generation run. A point is dropped, not deleted,
-- when a requirement changes, so the record of what was once accepted stays.
--
-- No FOREIGN KEY / cascade by repository rule: issue_id is validated in
-- application code, and deleting an issue sweeps its points and their links
-- in the same transaction.
CREATE TABLE IF NOT EXISTS issue_test_point (
    id                UUID NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    workspace_id      UUID NOT NULL,
    issue_id          UUID NOT NULL,
    number            INTEGER NOT NULL CHECK (number > 0),
    title             TEXT NOT NULL CHECK (btrim(title) <> ''),
    description       TEXT NOT NULL DEFAULT '',
    category          TEXT NOT NULL DEFAULT 'positive'
                      CHECK (category IN ('positive', 'negative', 'boundary', 'permission', 'state', 'data', 'non_functional')),
    origin            TEXT NOT NULL DEFAULT 'human' CHECK (origin IN ('ai', 'human')),
    status            TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'dropped')),
    generation_job_id UUID,
    created_by_type   TEXT NOT NULL DEFAULT 'member' CHECK (created_by_type IN ('member', 'agent')),
    created_by        UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
