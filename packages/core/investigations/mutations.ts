import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { investigationKeys } from "./queries";
import type { CreateInvestigationRequest } from "./types";

function useInvestigationMutation<T>(mutationFn: (value: T) => Promise<unknown>) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({ mutationFn, onSettled: () => qc.invalidateQueries({ queryKey: investigationKeys.all(wsId) }) });
}

export function useCreateInvestigation() {
  return useInvestigationMutation((data: CreateInvestigationRequest) => api.createInvestigation(data));
}

export function useAddInvestigationComment() {
  return useInvestigationMutation(({ id, content, attachmentIds }: { id: string; content: string; attachmentIds?: string[] }) => api.addInvestigationComment(id, content, attachmentIds));
}

export function useConfirmInvestigation() {
  return useInvestigationMutation((id: string) => api.confirmInvestigation(id));
}

export function useRetryInvestigation() {
  return useInvestigationMutation((id: string) => api.retryInvestigation(id));
}

export function useChangeInvestigationAgent() {
  return useInvestigationMutation(({ id, agentId }: { id: string; agentId: string }) => api.changeInvestigationAgent(id, agentId));
}

export function useLinkInvestigationProject() {
  return useInvestigationMutation(({ id, projectId }: { id: string; projectId: string }) => api.linkInvestigationProject(id, projectId));
}

export function useCreateInvestigationProject() {
  return useInvestigationMutation(({ id, title }: { id: string; title?: string }) => api.createInvestigationProject(id, title));
}

export function useInvestigationFeedback() {
  return useInvestigationMutation(({ id, checkpoint, score, attribution, comment }: { id: string; checkpoint: "diagnosis_confirmed" | "project_converted"; score: number; attribution?: string; comment?: string }) => api.submitInvestigationFeedback(id, checkpoint, score, attribution, comment));
}
