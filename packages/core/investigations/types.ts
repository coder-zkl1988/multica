export type InvestigationEnvironment = "test" | "production";
export type InvestigationStatus = "investigating" | "needs_input" | "awaiting_confirmation" | "completed";

export interface Investigation {
  id: string;
  workspace_id: string;
  title: string;
  description: string;
  environment: InvestigationEnvironment;
  agent_id: string;
  status: InvestigationStatus;
  current_task_id: string | null;
  root_cause: string | null;
  evidence: unknown[];
  confidence: "confirmed" | "provisional" | "unverified" | null;
  category: string | null;
  recommendations: string[];
  open_questions: string[];
  project_id: string | null;
  created_by: string;
  first_started_at: string | null;
  needs_input_at: string | null;
  conclusion_at: string | null;
  confirmed_at: string | null;
  converted_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface InvestigationComment {
  id: string;
  parent_id: string | null;
  author_type: "member" | "agent" | "system";
  author_id: string | null;
  content: string;
  type: string;
  task_id: string | null;
  created_at: string;
}

export interface InvestigationTask {
  id: string;
  status: string;
  failure_reason: string | null;
  attempt: number;
  created_at: string;
  started_at: string | null;
  completed_at: string | null;
}

export interface InvestigationDetail extends Investigation {
  comments: InvestigationComment[];
  tasks: InvestigationTask[];
  attachments: Attachment[];
}

export interface CreateInvestigationRequest {
  title?: string;
  description: string;
  environment: InvestigationEnvironment;
  agent_id: string;
  attachment_ids?: string[];
}

export interface InvestigationStatistics {
  created_count: number;
  started_count: number;
  completed_count: number;
  converted_count: number;
  failed_tasks: number;
  retried_tasks: number;
  diagnosis_feedback_count: number;
  diagnosis_average: number;
  project_feedback_count: number;
  project_average: number;
}
import type { Attachment } from "../types";
