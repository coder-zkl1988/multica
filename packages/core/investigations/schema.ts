import { z } from "zod";
import { AttachmentResponseSchema } from "../api/schemas";

const nullableString = z.string().nullable().default(null);

export const InvestigationSchema = z.object({
  id: z.string(), workspace_id: z.string(), title: z.string(), description: z.string(),
  environment: z.enum(["test", "production"]), agent_id: z.string(),
  status: z.enum(["investigating", "needs_input", "awaiting_confirmation", "completed"]),
  current_task_id: nullableString, root_cause: nullableString, evidence: z.array(z.unknown()).default([]),
  confidence: z.enum(["confirmed", "provisional", "unverified"]).nullable().default(null),
  category: nullableString, recommendations: z.array(z.string()).default([]), open_questions: z.array(z.string()).default([]),
  project_id: nullableString, created_by: z.string(), first_started_at: nullableString,
  needs_input_at: nullableString, conclusion_at: nullableString, confirmed_at: nullableString,
  converted_at: nullableString, created_at: z.string(), updated_at: z.string(),
});

export const InvestigationCommentSchema = z.object({
  id: z.string(), parent_id: nullableString, author_type: z.enum(["member", "agent", "system"]),
  author_id: nullableString, content: z.string(), type: z.string(), task_id: nullableString, created_at: z.string(),
});

export const InvestigationTaskSchema = z.object({
  id: z.string(), status: z.string(), failure_reason: nullableString, attempt: z.number(),
  created_at: z.string(), started_at: nullableString, completed_at: nullableString,
});

export const InvestigationDetailSchema = InvestigationSchema.extend({
  comments: z.array(InvestigationCommentSchema).default([]), tasks: z.array(InvestigationTaskSchema).default([]),
  attachments: z.array(AttachmentResponseSchema).default([]),
});

export const InvestigationStatisticsSchema = z.object({
  created_count: z.number(), started_count: z.number(), completed_count: z.number(), converted_count: z.number(),
  failed_tasks: z.number(), retried_tasks: z.number(), diagnosis_feedback_count: z.number(), diagnosis_average: z.number(),
  project_feedback_count: z.number(), project_average: z.number(),
});

export const InvestigationListSchema = z.array(InvestigationSchema);

export const EMPTY_INVESTIGATION = {
  id: "", workspace_id: "", title: "", description: "", environment: "production" as const,
  agent_id: "", status: "investigating" as const, current_task_id: null, root_cause: null,
  evidence: [], confidence: null, category: null, recommendations: [], open_questions: [],
  project_id: null, created_by: "", first_started_at: null, needs_input_at: null,
  conclusion_at: null, confirmed_at: null, converted_at: null, created_at: "", updated_at: "",
};

export const EMPTY_INVESTIGATION_STATISTICS = {
  created_count: 0, started_count: 0, completed_count: 0, converted_count: 0,
  failed_tasks: 0, retried_tasks: 0, diagnosis_feedback_count: 0, diagnosis_average: 0,
  project_feedback_count: 0, project_average: 0,
};
