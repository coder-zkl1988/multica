"use client";

import { useMemo, useState } from "react";
import { useSearchParams } from "next/navigation";
import { useMutation, useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { Button } from "@multica/ui/components/ui/button";

export default function FigmaPluginAuthorizePage() {
  const searchParams = useSearchParams();
  const sessionId = searchParams.get("session_id") ?? "";
  const { data: workspaces = [], isLoading, error } = useQuery({ queryKey: ["figma-plugin-authorize", "workspaces"], queryFn: () => api.listWorkspaces() });
  const [workspaceId, setWorkspaceId] = useState("");
  const selectedWorkspaceId = workspaceId || workspaces[0]?.id || "";
  const selectedWorkspace = useMemo(() => workspaces.find((workspace) => workspace.id === selectedWorkspaceId) ?? workspaces[0], [selectedWorkspaceId, workspaces]);
  const authorize = useMutation({ mutationFn: () => api.authorizeFigmaPluginAuthSession(sessionId, selectedWorkspaceId) });

  return (
    <main className="flex min-h-screen items-center justify-center bg-muted/30 px-4 py-10">
      <div className="w-full max-w-md rounded-xl border bg-background p-6 shadow-sm">
        <div className="text-body font-semibold">授权 Figma 插件</div>
        <p className="mt-2 text-body text-muted-foreground">允许 Figma 插件将 Gallery Native 设计稿上传到一个 Multica 工作区。</p>

        {!sessionId ? <div className="mt-4 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-body text-destructive">缺少授权会话。</div> : null}
        {error ? <div className="mt-4 rounded-md border p-3 text-body text-muted-foreground">请先登录 Multica，然后重新打开此授权链接。</div> : null}

        <label className="mt-5 block text-caption font-medium text-muted-foreground">工作区</label>
        <select
          value={selectedWorkspaceId}
          onChange={(event) => setWorkspaceId(event.target.value)}
          disabled={isLoading || authorize.isSuccess}
          className="mt-2 h-9 w-full rounded-md border bg-background px-3 text-body"
        >
          {workspaces.map((workspace) => <option key={workspace.id} value={workspace.id}>{workspace.name}</option>)}
        </select>

        <div className="mt-5 rounded-md bg-muted/50 p-3 text-caption text-muted-foreground">
          此凭证仅限用于 {selectedWorkspace?.name ?? "所选工作区"}，并且只能创建设计稿导入。
        </div>

        {authorize.isSuccess ? (
          <div className="mt-5 rounded-md border border-emerald-500/30 bg-emerald-500/10 p-3 text-body text-emerald-700 dark:text-emerald-300">已授权。请返回 Figma，插件会自动完成登录。</div>
        ) : (
          <Button className="mt-5 w-full" disabled={!sessionId || !selectedWorkspaceId || authorize.isPending} onClick={() => authorize.mutate()}>
            {authorize.isPending ? "授权中…" : "授权 Figma 插件"}
          </Button>
        )}
      </div>
    </main>
  );
}
