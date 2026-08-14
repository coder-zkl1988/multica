# PMO Run Transcript Fix Plan

1. Add Go regression tests for reading the current OpenClaw session turn and mapping thinking/tool calls/tool results.
2. Make transcript collection fail-open and keep the stdout final response as the last message.
3. Add PMO run-history tests and reuse the existing TranscriptButton for runs with agent_task_id.
4. Run targeted Go/frontend tests, typecheck, diff checks, and GitNexus change detection when available.
