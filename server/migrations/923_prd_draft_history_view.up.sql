-- Fork-local (800+): read model for workspace PRD analytics. Rows are
-- projections of chat_prd_draft keyed by topic; history is derivable from
-- the existing tables, so this view needs no ownership of its own.
CREATE VIEW chat_prd_draft_history AS
SELECT d.id,
       d.workspace_id,
       d.installation_id,
       d.channel_chat_id,
       d.channel_thread_id,
       d.source_message_id,
       d.initiator_open_id,
       b.multica_user_id AS initiator_multica_user_id,
       d.version,
       d.content,
       d.confirmed_content,
       d.confirmation_message_id,
       d.status,
       d.phase,
       d.document_id,
       d.document_url,
       d.failure,
       d.version_created_at,
       d.created_at,
       d.updated_at
FROM chat_prd_draft d
LEFT JOIN channel_user_binding b
  ON b.installation_id = d.installation_id
 AND b.channel_type = 'feishu'
 AND b.channel_user_id = d.initiator_open_id;
