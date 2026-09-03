-- Restore the pre-441 whitelist. PostgreSQL's NOT VALID only skips checking
-- rows that already exist; it still rejects new rows and updates that violate
-- the constraint. Refuse the rollback while CodeArts data is present instead
-- of leaving an apparently valid migration that cannot preserve that data.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM runtime_profile WHERE protocol_family = 'codearts') THEN
        RAISE EXCEPTION 'cannot roll back CodeArts protocol support while runtime_profile rows exist';
    END IF;
END
$$;

ALTER TABLE runtime_profile DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check;

ALTER TABLE runtime_profile ADD CONSTRAINT runtime_profile_protocol_family_check
    CHECK (protocol_family IN (
        'claude',
        'codebuddy',
        'codex',
        'copilot',
        'opencode',
        'openclaw',
        'hermes',
        'pi',
        'cursor',
        'kimi',
        'reasonix',
        'dsh',
        'kiro',
        'antigravity',
        'qoder',
        'qoderclicn',
        'traecli',
        'deveco',
        'grok',
        'qwen',
        'qwenpaw',
        'mcode',
        'dim',
        'zeroclaw'
    )) NOT VALID;
