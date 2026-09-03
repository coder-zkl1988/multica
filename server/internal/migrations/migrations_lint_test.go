package migrations

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const maxLegacyMigrationPrefix = 148

// Fork migration numbering discipline (full rule in CLAUDE.md「Database and
// Migration Rules」): this repo is a fork that merges multica-ai/multica
// regularly, and both sides advancing the same counter is how the 255–279,
// 281–284, and 313–317 prefix collisions happened (280 is a fork gap-fill, not a collision). New fork-local migrations take prefixes from
// forkMigrationPrefixStart (800) upward; never take the next number after
// upstream's latest.
const forkMigrationPrefixStart = 800

// lastUpstreamMigrationPrefix is the highest prefix owned by multica-ai/multica
// upstream at the fork's most recent upstream merge. Bump it in the
// upstream-sync PR whenever upstream adds migrations, so the range between it
// and forkMigrationPrefixStart stays empty except for the pre-existing fork
// migrations recorded in existingForkMigrationPrefixes.
//
// Known limitation: a new fork migration that picks a prefix inside an upstream
// numbering gap would pass this check; the uniqueness test still catches it
// once upstream owns that number. The realistic violation (anything between
// lastUpstreamMigrationPrefix and 800) is rejected below.
// Bumped from 445 with the upstream/main sync that brought 446–448.
const lastUpstreamMigrationPrefix = 448

// existingForkMigrationPrefixes are fork-local migrations that were applied to
// production before the 800+ rule; they keep their numbers forever because the
// runner keys schema_migrations on the full stem and a rename would re-apply
// them. Never add new entries here — new fork migrations use 800+.
var existingForkMigrationPrefixes = map[string]bool{
	"280": true, // 280_test_case
	"285": true, // 285_test_case_repo_case_index
	"286": true, // 286_test_case_repo_resource_index
	"287": true, // 287_test_case_module_index
	"288": true, // 288_test_generation
	"289": true, // 289_test_generation_job_workspace_index
	"290": true, // 290_test_generation_job_agent_task_index
	"291": true, // 291_test_generation_job_project_status_index
	"292": true, // 292_test_generation_plan_live_index
	"293": true, // 293_test_case_proposal_target_index
	"294": true, // 294_test_case_proposal_job_index
	"295": true, // 295_test_run
	"296": true, // 296_test_plan_project_index
	"297": true, // 297_test_plan_case_order_index
	"298": true, // 298_test_run_project_index
	"299": true, // 299_test_run_agent_task_index
	"300": true, // 300_test_run_source_index
	"301": true, // 301_test_run_case_order_index
	"302": true, // 302_test_run_case_timeline_index
	"303": true, // 303_attachment_test_run_case_index
	"304": true, // 304_test_capability_key_index
	"305": true, // 305_test_capability_kind_index
	"306": true, // 306_pmo_sync_tables
	"307": true, // 307_pmo_sync_config_id_index
	"308": true, // 308_pmo_sync_run_id_index
	"309": true, // 309_pmo_sync_link_id_index
	"310": true, // 310_pmo_sync_primary_keys
	"311": true, // 311_pmo_sync_config_root_index
	"312": true, // 312_pmo_sync_run_history_index
	"313": true, // 313_pmo_sync_run_active_index
	"314": true, // 314_pmo_sync_run_agent_task_index
	"315": true, // 315_pmo_sync_link_identity_index
	"316": true, // 316_project_created_by
	"317": true, // 317_product_map
}

// legacyDuplicateMigrationStems lists prefixes that were already duplicated
// before this lint existed. It is a frozen historical record, not an escape
// hatch: a new collision must be renumbered instead of added here. Prefix 362
// was briefly listed and is deliberately absent again — the later of the two
// migrations was renumbered to 376, which its idempotent DDL made safe.
var legacyDuplicateMigrationStems = map[string][]string{
	"020": {"020_issue_number", "020_task_session"},
	"026": {"026_comment_reactions", "026_task_messages"},
	"029": {"029_attachment", "029_daemon_token", "029_drop_daemon_pairing"},
	"032": {"032_drop_agent_triggers", "032_issue_search_index", "032_runtime_owner", "032_task_usage"},
	"033": {"033_chat", "033_comment_search_index"},
	"035": {"035_project_priority", "035_task_queue_issue_id_index"},
	"040": {"040_agent_custom_env", "040_chat_unread_since"},
	"041": {"041_agent_custom_args", "041_workspace_invitation"},
	"043": {"043_audit_reserved_slugs", "043_fix_orphaned_autopilot_runs"},
	"046": {"046_agent_mcp_config", "046_agent_unique_name", "046_drop_runtime_usage"},
	"050": {"050_add_onboarded_at_to_users", "050_agent_model", "050_issue_first_executed_at"},
	"060": {"060_add_user_language", "060_agent_description_length", "060_chat_session_runtime_id", "060_issue_origin_quick_create"},
	"065": {"065_backfill_onboarded_at", "065_project_resources"},
	"069": {"069_comment_resolved_at", "069_drop_task_last_heartbeat"},
	"079": {"079_autopilot_run_skipped_status", "079_backfill_api_invalid_request", "079_github_integration"},
	"083": {"083_attachment_chat_columns", "083_runtime_visibility"},
	"084": {"084_squad", "084_task_usage_dashboard_rollup"},
	"091": {"091_autopilot_webhook_triggers", "091_issue_start_date", "091_pr_ci_conflict"},
	"095": {"095_agent_thinking_level", "095_backfill_starter_content_state"},
	"096": {"096_autopilot_squad_assignee", "096_pending_check_suite", "096_user_profile_description"},
	"098": {"098_contact_sales_inquiries", "098_user_onboarding_runtime_choice"},
	"109": {"109_agent_task_waiting_local_directory", "109_drop_agent_skills_local", "109_issue_pull_request_close_intent", "109_lark_integration"},
	"111": {"111_issue_origin_lark_chat", "111_workspace_avatar"},
	"112": {"112_issue_dates_to_date", "112_lark_installation_bot_union_id"},
	"113": {"113_lark_inbound_dedup_per_installation", "113_sys_cron_executions"},
	"120": {"120_autopilot_subscriber", "120_comment_source_task_id", "120_github_pending_installation", "120_runtime_profile"},
	"122": {"122_lark_chat_session_binding_thread_reply", "122_task_handoff_note"},
	"124": {"124_autopilot_run_planned_at", "124_channel_generalization", "124_task_prepare_lease"},
	"127": {"127_issue_pull_request_reference_only", "127_task_squad_id", "127_user_composio_connection"},
	"128": {"128_agent_task_queue_runtime_mcp_overlay", "128_autopilot_collaborator", "128_comment_routing_escalation"},
}

// mergedDuplicateMigrationStems records prefix collisions created by merging
// upstream into this fork, where BOTH sides had already been applied to a live
// database before the merge. The runner keys schema_migrations on the full stem
// (see ExtractVersion), not the numeric prefix, so these apply and record
// independently — nothing is skipped. Renumbering either side would change its
// stem and make an applied migration run a second time, which for a
// CREATE INDEX CONCURRENTLY is a hard failure.
//
// This is NOT an escape hatch for new work. A collision qualifies only when
// both stems have already shipped and can no longer be renamed. Anything still
// in review takes the next free prefix instead — the whole point of the
// uniqueness rule is that ordering between two same-numbered files is decided
// by the rest of the filename, which is alphabetical accident rather than
// intent.
var mergedDuplicateMigrationStems = map[string][]string{
	"255": {"255_agent_task_queue_chat_pending_deferred_v3", "255_service_account_token_primary_key"},
	"256": {"256_drop_agent_task_queue_chat_pending_v2", "256_service_account_token_hash_index"},
	"257": {"257_agent_task_queue_channel_media_pending_unique_v2", "257_service_account_token_active_index"},
	"258": {"258_drop_pending_issue_agent_v1", "258_user_service_account_index"},
	"259": {"259_agent_task_queue_retired_session_id", "259_issue_origin_dingtalk_chat"},
	"260": {"260_chat_message_quick_actions", "260_issue_origin_dingtalk_chat_validate"},
	"261": {"261_agent_task_queue_terminal_completed_at_v2", "261_agent_task_quick_actions_disabled"},
	"262": {"262_drop_agent_task_queue_terminal_completed_at_v1", "262_quick_action"},
	"263": {"263_issue_origin_wecom_chat", "263_quick_action_workspace_index"},
	"264": {"264_comment_quick_action", "264_issue_origin_wecom_chat_validate"},
	"265": {"265_agent_task_regenerate_quick_actions", "265_issue_view"},
	"266": {"266_comment_parent_lookup_index", "266_issue_view_owner_index"},
	"267": {"267_issue_view_shared_index", "267_runtime_profile_add_qoderclicn"},
	"268": {"268_issue_view_preference", "268_workspace_teardown_dirty_trigger_guard"},
	"269": {"269_issue_dependency_issue_index", "269_issue_view_workspace_variant"},
	"270": {"270_issue_dependency_depends_on_index", "270_pinned_item_view"},
	"271": {"271_channel_chat_pending_fresh", "271_inbox_item_issue_index"},
	"272": {"272_comment_parent_index", "272_rollup_task_usage_hourly_xact_lock"},
	"273": {"273_agent_task_queue_runtime_id_index", "273_agent_task_trigger_comment_index"},
	"274": {"274_issue_subscriber_delegated", "274_task_token_workspace_id_index"},
	"275": {"275_issue_subscriber_opt_out_scope", "275_task_token_agent_id_index"},
	"276": {"276_agent_runtime_unbind", "276_chat_draft_restore_task_id_index"},
	"277": {"277_agent_builder_draft", "277_autopilot_run_task_id_index"},
	"278": {"278_agent_task_queue_agent_id_keyset_index", "278_runtime_profile_add_qwenpaw"},
	"279": {"279_agent_task_queue_issue_id_keyset_index", "279_runtime_profile_add_reasonix"},
	"281": {"281_agent_workspace_id_keyset_index", "281_test_case_workspace_number_index"},
	"282": {"282_issue_workspace_id_keyset_index", "282_test_case_project_status_index"},
	"283": {"283_agent_runtime_workspace_id_keyset_index", "283_test_case_generation_job_index"},
	"284": {"284_task_owner_row_fence", "284_test_case_revision_case_index"},
	"285": {"285_plugin_lifecycle_v1", "285_test_case_repo_case_index"},
	"286": {"286_plugin_identity_key_index", "286_test_case_repo_resource_index"},
	"287": {"287_plugin_release_version_index", "287_test_case_module_index"},
	"288": {"288_plugin_installation_workspace_plugin_index", "288_test_generation"},
	"289": {"289_plugin_contribution_key_index", "289_test_generation_job_workspace_index"},
	"290": {"290_plugin_contribution_ordinal_index", "290_test_generation_job_agent_task_index"},
	"291": {"291_plugin_grant_revision_index", "291_test_generation_job_project_status_index"},
	"292": {"292_plugin_binding_revision_index", "292_test_generation_plan_live_index"},
	"293": {"293_plugin_installation_workspace_index", "293_test_case_proposal_target_index"},
	"294": {"294_plugin_snapshot_execution_v1", "294_test_case_proposal_job_index"},
	"295": {"295_plugin_artifact_file_index", "295_test_run"},
	"296": {"296_plugin_snapshot_revision_index", "296_test_plan_project_index"},
	"297": {"297_plugin_execution_task_index", "297_test_plan_case_order_index"},
	"298": {"298_plugin_health_index", "298_test_run_project_index"},
	"299": {"299_agent_task_plugin_manifest_index", "299_test_run_agent_task_index"},
	"300": {"300_drop_redundant_issue_workspace_number_index", "300_test_run_source_index"},
	"301": {"301_drop_redundant_sys_cron_job_plan_index", "301_test_run_case_order_index"},
	"302": {"302_drop_redundant_channel_chat_session_binding_index", "302_test_run_case_timeline_index"},
	"303": {"303_attachment_test_run_case_index", "303_drop_redundant_lark_chat_session_binding_index"},
	"304": {"304_dingtalk_group_route", "304_test_capability_key_index"},
	"305": {"305_dingtalk_group_route_installation_conversation_unique", "305_test_capability_kind_index"},
	"306": {"306_dingtalk_group_route_workspace_index", "306_pmo_sync_tables"},
	"307": {"307_dingtalk_group_route_id_unique", "307_pmo_sync_config_id_index"},
	"308": {"308_agent_task_branch_name", "308_pmo_sync_run_id_index"},
	"309": {"309_agent_runtime_id_index", "309_pmo_sync_link_id_index"},
	"310": {"310_pmo_sync_primary_keys", "310_workspace_private_plugin_identity"},
	"311": {"311_plugin_identity_scoped_key_index", "311_pmo_sync_config_root_index"},
	"312": {"312_drop_global_plugin_identity_key_index", "312_pmo_sync_run_history_index"},
	"313": {"313_pmo_sync_run_active_index", "313_runtime_profile_add_dsh"},
	"314": {"314_pmo_sync_run_agent_task_index", "314_workspace_mcp_config"},
	"315": {"315_pmo_sync_link_identity_index", "315_workspace_mcp_server"},
	"316": {"316_project_created_by", "316_workspace_mcp_server_name_unique"},
	"317": {"317_agent_mcp_server_server_index", "317_product_map"},
}

// legacyFKMigrations records already-applied migrations that create database
// foreign keys / cascading deletes, grandfathered before the no-FK rule. New
// migrations must never be added here — database relationships must be
// resolved in application code (CLAUDE.md「Database and Migration Rules」).
var legacyFKMigrations = map[string]bool{
	"001_init":                                true,
	"004_agent_runtime_loop":                  true,
	"005_daemon_pairing":                      true,
	"008_structured_skills":                   true,
	"011_personal_access_tokens":              true,
	"013_runtime_usage":                       true,
	"015_issue_subscriber":                    true,
	"017_comment_parent_id":                   true,
	"018_comment_parent_cascade":              true,
	"025_comment_workspace_id":                true,
	"026_comment_reactions":                   true,
	"026_task_messages":                       true,
	"027_issue_reactions":                     true,
	"028_task_trigger_comment":                true,
	"029_attachment":                          true,
	"029_daemon_token":                        true,
	"031_agent_archive":                       true,
	"032_runtime_owner":                       true,
	"032_task_usage":                          true,
	"033_chat":                                true,
	"034_projects":                            true,
	"038_pinned_items":                        true,
	"041_workspace_invitation":                true,
	"042_autopilot":                           true,
	"055_task_lease_and_retry":                true,
	"057_feedback":                            true,
	"060_chat_session_runtime_id":             true,
	"064_notification_preference":             true,
	"065_project_resources":                   true,
	"079_github_integration":                  true,
	"083_attachment_chat_columns":             true,
	"084_squad":                               true,
	"091_pr_ci_conflict":                      true,
	"093_webhook_deliveries":                  true,
	"096_autopilot_squad_assignee":            true,
	"097_autopilot_project_id":                true,
	"108_task_token":                          true,
	"109_lark_integration":                    true,
	"113_lark_inbound_dedup_per_installation": true,
	"234_gallery_native_designs":              true,
	"235_figma_import_codes":                  true,
	"236_design_plugin_auth":                  true,
	"237_design_project_folders":              true,
	"238_design_template_catalog":             true,
	"239_design_draft_template_catalog":       true,
	"240_design_draft_materialized_refs":      true,
	"241_design_restore_plan":                 true,
	"242_design_repo_analysis":                true,
	"243_design_delivery":                     true,
	"244_design_restore_task_delivery":        true,
	"247_design_system_profile":               true,
	"317_product_map":                         true,
}

// legacyNonConcurrentIndexMigrations records already-applied migrations that
// build indexes with plain CREATE INDEX (not CONCURRENTLY) and/or pack
// multiple statements into one file, grandfathered before the rule. New
// migrations must never be added here.
var legacyNonConcurrentIndexMigrations = map[string]bool{
	"001_init":                                         true,
	"003_task_context":                                 true,
	"004_agent_runtime_loop":                           true,
	"005_daemon_pairing":                               true,
	"008_structured_skills":                            true,
	"009_verification_code":                            true,
	"011_personal_access_tokens":                       true,
	"013_runtime_usage":                                true,
	"015_issue_subscriber":                             true,
	"020_issue_number":                                 true,
	"022_task_lifecycle_guards":                        true,
	"026_comment_reactions":                            true,
	"026_task_messages":                                true,
	"027_issue_reactions":                              true,
	"029_attachment":                                   true,
	"029_daemon_token":                                 true,
	"032_issue_search_index":                           true,
	"032_task_usage":                                   true,
	"033_chat":                                         true,
	"033_comment_search_index":                         true,
	"034_projects":                                     true,
	"036_search_index_lower":                           true,
	"037_fix_pending_task_unique_index":                true,
	"038_pinned_items":                                 true,
	"039_project_search_index":                         true,
	"040_chat_unread_since":                            true,
	"041_workspace_invitation":                         true,
	"042_autopilot":                                    true,
	"043_fix_orphaned_autopilot_runs":                  true,
	"050_issue_first_executed_at":                      true,
	"055_task_lease_and_retry":                         true,
	"057_feedback":                                     true,
	"059_label_timestamps":                             true,
	"065_project_resources":                            true,
	"068_timeline_keyset_index":                        true,
	"069_comment_resolved_at":                          true,
	"073_task_usage_daily_rollup":                      true,
	"077_task_usage_daily_invalidation":                true,
	"079_github_integration":                           true,
	"083_attachment_chat_columns":                      true,
	"084_squad":                                        true,
	"084_task_usage_dashboard_rollup":                  true,
	"089_squad_no_action_activity_index":               true,
	"091_autopilot_webhook_triggers":                   true,
	"091_pr_ci_conflict":                               true,
	"093_webhook_deliveries":                           true,
	"096_autopilot_squad_assignee":                     true,
	"096_pending_check_suite":                          true,
	"097_autopilot_project_id":                         true,
	"098_contact_sales_inquiries":                      true,
	"101_task_usage_hourly_schema":                     true,
	"105_issue_metadata":                               true,
	"108_task_token":                                   true,
	"109_lark_integration":                             true,
	"113_lark_inbound_dedup_per_installation":          true,
	"113_sys_cron_executions":                          true,
	"120_autopilot_subscriber":                         true,
	"120_runtime_profile":                              true,
	"121_agent_runtime_provider_partial_unique":        true,
	"124_autopilot_run_planned_at":                     true,
	"124_channel_generalization":                       true,
	"127_task_squad_id":                                true,
	"127_user_composio_connection":                     true,
	"128_autopilot_collaborator":                       true,
	"128_comment_routing_escalation":                   true,
	"129_agent_composio_allowlist_and_task_originator": true,
	"130_agent_invocation_permission":                  true,
	"133_github_installation_multi_workspace":          true,
	"234_gallery_native_designs":                       true,
	"235_figma_import_codes":                           true,
	"236_design_plugin_auth":                           true,
	"237_design_project_folders":                       true,
	"238_design_template_catalog":                      true,
	"239_design_draft_template_catalog":                true,
	"240_design_draft_materialized_refs":               true,
	"241_design_restore_plan":                          true,
	"242_design_repo_analysis":                         true,
	"243_design_delivery":                              true,
	"244_design_restore_task_delivery":                 true,
	"246_design_delivery_single_active_source":         true,
	"247_design_system_profile":                        true,
	"317_product_map":                                  true,
}

var migrationPrefixPattern = regexp.MustCompile(`^(\d+)_`)

func TestMigrationFilesHaveMatchingDirections(t *testing.T) {
	files := migrationFilesForLint(t, "*.sql")

	directionsByStem := make(map[string]map[string]bool)
	for _, file := range files {
		stem, direction, ok := splitMigrationFilename(filepath.Base(file))
		if !ok {
			continue
		}
		if directionsByStem[stem] == nil {
			directionsByStem[stem] = make(map[string]bool)
		}
		directionsByStem[stem][direction] = true
	}

	for stem, directions := range directionsByStem {
		if !directions["up"] || !directions["down"] {
			t.Errorf("migration %s must have both .up.sql and .down.sql files", stem)
		}
	}
}

func TestMigrationNumericPrefixesStayUniqueAfterLegacySet(t *testing.T) {
	stemsByPrefix := migrationStemsByPrefix(t)

	for prefix, stems := range stemsByPrefix {
		sort.Strings(stems)

		allowed, kind := allowedDuplicateMigrationStems(prefix)
		if kind != "" {
			expected := append([]string(nil), allowed...)
			sort.Strings(expected)
			if !reflect.DeepEqual(stems, expected) {
				t.Errorf("%s duplicate migration prefix %s changed: got %v, want %v; do not add to or rename already-applied duplicate-prefix migrations", kind, prefix, stems, expected)
			}
			continue
		}

		if len(stems) > 1 {
			t.Errorf("migration prefix %s is reused by %v; use the next unique prefix instead", prefix, stems)
		}
	}
}

func TestNewForkMigrationsUseReservedRange(t *testing.T) {
	stemsByPrefix := migrationStemsByPrefix(t)

	for prefix, stems := range stemsByPrefix {
		n, err := strconv.Atoi(prefix)
		if err != nil {
			t.Fatalf("parse migration prefix %q: %v", prefix, err)
		}

		if n >= forkMigrationPrefixStart {
			continue // fork-reserved range
		}
		if isKnownLegacyPrefix(prefix) {
			continue // frozen legacy range 001-148
		}
		if existingForkMigrationPrefixes[prefix] {
			continue // pre-existing fork migration, keeps its number
		}
		if _, ok := mergedDuplicateMigrationStems[prefix]; ok {
			continue // recorded upstream/fork collision, both already applied
		}
		if n <= lastUpstreamMigrationPrefix {
			continue // merged in from multica-ai/multica upstream
		}

		t.Errorf("migration prefix %s is below the fork-reserved range %d: %v; either a new fork-local migration used a non-reserved prefix (use %d+) or upstream added migrations past lastUpstreamMigrationPrefix=%d (bump it in the sync PR)", prefix, forkMigrationPrefixStart, stems, forkMigrationPrefixStart, lastUpstreamMigrationPrefix)
	}
}

func TestExistingForkMigrationPrefixesExist(t *testing.T) {
	files := migrationFilesForLint(t, "*.up.sql")
	seen := make(map[string]bool)
	for _, file := range files {
		stem := strings.TrimSuffix(filepath.Base(file), ".up.sql")
		if match := migrationPrefixPattern.FindStringSubmatch(stem); match != nil {
			seen[match[1]] = true
		}
	}
	for prefix := range existingForkMigrationPrefixes {
		if !seen[prefix] {
			t.Errorf("existingForkMigrationPrefixes records prefix %s but no migration file with that prefix exists; remove the stale entry", prefix)
		}
	}
}

func TestNewMigrationPrefixesStartAfterLegacyRange(t *testing.T) {
	stemsByPrefix := migrationStemsByPrefix(t)

	for prefix, stems := range stemsByPrefix {
		n, err := strconv.Atoi(prefix)
		if err != nil {
			t.Fatalf("parse migration prefix %q: %v", prefix, err)
		}
		if n <= maxLegacyMigrationPrefix && !isKnownLegacyPrefix(prefix) {
			t.Errorf("migration prefix %s is in the frozen legacy range 001-%03d: %v; new migrations must start at %03d", prefix, maxLegacyMigrationPrefix, stems, maxLegacyMigrationPrefix+1)
		}
	}
}

var fkKeywordPattern = regexp.MustCompile(`\bREFERENCES\b|\bFOREIGN\s+KEY\b`)

func TestNoForeignKeysOutsideLegacySet(t *testing.T) {
	files := migrationFilesForLint(t, "*.up.sql")
	for _, file := range files {
		stem := strings.TrimSuffix(filepath.Base(file), ".up.sql")
		if legacyFKMigrations[stem] {
			continue
		}
		upper := strings.ToUpper(stripSQLStringLiterals(stripSQLComments(readMigrationForLint(t, filepath.Base(file)))))
		if fkKeywordPattern.MatchString(upper) {
			t.Errorf("%s must not create foreign keys (REFERENCES/FOREIGN KEY); resolve relationships in application code. If this is an already-applied legacy migration, record it in legacyFKMigrations", stem)
		}
	}
}

func TestIndexesUseConcurrentlyOutsideLegacySet(t *testing.T) {
	files := migrationFilesForLint(t, "*.up.sql")
	for _, file := range files {
		stem := strings.TrimSuffix(filepath.Base(file), ".up.sql")
		if legacyNonConcurrentIndexMigrations[stem] {
			continue
		}
		sql := strings.TrimSpace(stripSQLComments(readMigrationForLint(t, filepath.Base(file))))
		upper := strings.ToUpper(sql)
		if !strings.Contains(upper, "CREATE INDEX") && !strings.Contains(upper, "CREATE UNIQUE INDEX") {
			continue // not an index migration
		}
		if !strings.Contains(upper, "CREATE UNIQUE INDEX CONCURRENTLY") &&
			!strings.Contains(upper, "CREATE INDEX CONCURRENTLY") {
			t.Errorf("%s must create its index concurrently (CREATE [UNIQUE] INDEX CONCURRENTLY); see CLAUDE.md", stem)
		}
		if strings.Count(sql, ";") != 1 {
			t.Errorf("%s must contain one statement; each concurrent index build needs its own single-statement migration file", stem)
		}
	}
}

// stripSQLComments removes SQL line (--) and block (/* */) comments so lint
// rules only see executable statements, not prose that happens to mention
// keywords like "references", "preferences" or "index". Heuristic only; a
// literal containing -- or /* would be misread, which no migration here does.
func stripSQLComments(sql string) string {
	var b strings.Builder
	b.Grow(len(sql))
	i := 0
	for i < len(sql) {
		if i+1 < len(sql) && sql[i] == '-' && sql[i+1] == '-' {
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(sql) && sql[i] == '/' && sql[i+1] == '*' {
			i += 2
			for i+1 < len(sql) && !(sql[i] == '*' && sql[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}
		b.WriteByte(sql[i])
		i++
	}
	return b.String()
}

// stripSQLStringLiterals blanks the contents of single-quoted literals,
// keeping the quotes so statement structure is unchanged.
//
// Structural lints must read DDL, not data. A seed migration inserts arbitrary
// prose — one Open Design template's description cites `references/layouts.md`
// — and matching keywords inside that text reports a foreign key in a file
// that only ever runs INSERT. A real REFERENCES clause is never inside a
// literal, so removing literal contents cannot hide one.
func stripSQLStringLiterals(sql string) string {
	var b strings.Builder
	b.Grow(len(sql))
	for i := 0; i < len(sql); {
		if sql[i] != '\'' {
			b.WriteByte(sql[i])
			i++
			continue
		}
		b.WriteByte('\'')
		i++
		for i < len(sql) {
			// '' is an escaped quote inside the literal, not the end of it.
			if sql[i] == '\'' && i+1 < len(sql) && sql[i+1] == '\'' {
				i += 2
				continue
			}
			if sql[i] == '\'' {
				break
			}
			i++
		}
		if i < len(sql) {
			b.WriteByte('\'')
			i++
		}
	}
	return b.String()
}

func readMigrationForLint(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(realMigrationsDir(t), name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func migrationStemsByPrefix(t *testing.T) map[string][]string {
	t.Helper()

	files := migrationFilesForLint(t, "*.up.sql")
	stemsByPrefix := make(map[string][]string)
	for _, file := range files {
		stem := strings.TrimSuffix(filepath.Base(file), ".up.sql")
		match := migrationPrefixPattern.FindStringSubmatch(stem)
		if match == nil {
			t.Fatalf("migration %s does not start with a numeric prefix followed by underscore", stem)
		}
		stemsByPrefix[match[1]] = append(stemsByPrefix[match[1]], stem)
	}
	return stemsByPrefix
}

func migrationFilesForLint(t *testing.T, pattern string) []string {
	t.Helper()

	dir := realMigrationsDir(t)
	files, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no migration files matched %s in %s", pattern, dir)
	}
	sort.Strings(files)
	return files
}

func realMigrationsDir(t *testing.T) string {
	t.Helper()

	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve migration lint test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(self), "..", "..", "migrations"))
}

func splitMigrationFilename(name string) (stem, direction string, ok bool) {
	for _, candidateDirection := range []string{"up", "down"} {
		suffix := fmt.Sprintf(".%s.sql", candidateDirection)
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix), candidateDirection, true
		}
	}
	return "", "", false
}

// allowedDuplicateMigrationStems returns the recorded stems for a prefix that
// is permitted to be duplicated, plus a label naming why. An empty label means
// the prefix must be unique.
func allowedDuplicateMigrationStems(prefix string) ([]string, string) {
	if stems, ok := legacyDuplicateMigrationStems[prefix]; ok {
		return stems, "legacy"
	}
	if stems, ok := mergedDuplicateMigrationStems[prefix]; ok {
		return stems, "merged"
	}
	return nil, ""
}

func isKnownLegacyPrefix(prefix string) bool {
	if _, ok := legacyDuplicateMigrationStems[prefix]; ok {
		return true
	}

	switch prefix {
	case "001", "002", "003", "004", "005", "006", "007", "008", "009", "010",
		"011", "012", "013", "014", "015", "016", "017", "018", "019", "021",
		"022", "023", "024", "025", "027", "028", "030", "031", "034", "036",
		"037", "038", "039", "042", "044", "045", "047", "048", "049", "051",
		"052", "053", "054", "055", "056", "057", "058", "059", "061", "062",
		"063", "064", "066", "067", "068", "072", "073", "074", "075", "076",
		"077", "078", "080", "081", "082", "085", "086", "087", "088", "089",
		"090", "092", "093", "094", "097", "100", "101", "102", "103", "104",
		"105", "106", "107", "108", "110", "114", "115", "116", "117", "118",
		"119", "121", "123", "125", "126", "129", "130", "131", "132", "133",
		"134", "135", "136", "137", "138", "139", "140", "141", "142", "143",
		"144", "145", "146", "147", "148":
		return true
	default:
		return false
	}
}

// The literal stripper exists so seed prose cannot trip the FK lint. It must
// not become a way to smuggle a real foreign key past it.
func TestStripSQLStringLiteralsKeepsDDLVisible(t *testing.T) {
	cases := []struct {
		name    string
		sql     string
		wantHit bool
	}{
		{"real FK stays visible", "ALTER TABLE a ADD CONSTRAINT c FOREIGN KEY (b) REFERENCES t(id);", true},
		{"real REFERENCES stays visible", "CREATE TABLE a (b UUID REFERENCES t(id));", true},
		{"keyword inside prose is ignored", "INSERT INTO t (s) VALUES ('see references/layouts.md');", false},
		{"escaped quote does not end the literal", "INSERT INTO t (s) VALUES ('it''s references/x.md');", false},
		{"prose literal does not hide a following FK", "INSERT INTO t (s) VALUES ('references/x.md');\nCREATE TABLE a (b UUID REFERENCES t(id));", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upper := strings.ToUpper(stripSQLStringLiterals(stripSQLComments(tc.sql)))
			if got := fkKeywordPattern.MatchString(upper); got != tc.wantHit {
				t.Fatalf("FK detection = %v, want %v (stripped: %q)", got, tc.wantHit, upper)
			}
		})
	}
}
