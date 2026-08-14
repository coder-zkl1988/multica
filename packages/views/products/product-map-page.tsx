"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Network, ChevronRight, ChevronDown, CircleDot } from "lucide-react";
import { productMapTreeOptions, type ProductMapNode } from "@multica/core/products";
import { useWorkspaceId } from "@multica/core/hooks";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Card } from "@multica/ui/components/ui/card";
import { Badge } from "@multica/ui/components/ui/badge";
import { useT } from "../i18n";

import { ProductNodeDetail } from "./product-node-detail";

export function ProductStatusBadge({ node }: { node: ProductMapNode }) {
  const { t } = useT("products");
  const tone =
    node.status === "released"
      ? "default"
      : node.status === "pending_confirmation"
        ? "secondary"
        : "outline";
  const label =
    node.status === "released"
      ? t(($) => $.status.released)
      : node.status === "pending_confirmation"
        ? t(($) => $.status.pending_confirmation)
        : node.status === "pending_release"
          ? t(($) => $.status.pending_release)
          : t(($) => $.status.dev);
  return (
    <Badge variant={tone}>
      {label}
      {node.has_live_evidence ? t(($) => $.evidence_suffix) : ""}
    </Badge>
  );
}

function TreeNode({
  node,
  depth,
  selectedId,
  onSelect,
}: {
  node: ProductMapNode;
  depth: number;
  selectedId: string | null;
  onSelect: (id: string) => void;
}) {
  const [open, setOpen] = useState(depth === 0);
  const hasChildren = (node.children?.length ?? 0) > 0;
  const selected = selectedId === node.id;

  return (
    <div>
      <button
        type="button"
        onClick={() => {
          onSelect(node.id);
          if (hasChildren) setOpen((v) => !v);
        }}
        className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-body hover:bg-accent ${
          selected ? "bg-accent text-accent-foreground" : ""
        }`}
        style={{ paddingLeft: `${depth * 16 + 8}px` }}
      >
        {hasChildren ? (
          open ? (
            <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground" />
          ) : (
            <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground" />
          )
        ) : (
          <CircleDot className="h-4 w-4 shrink-0 text-muted-foreground" />
        )}
        <span className="truncate font-medium">{node.name}</span>
        <ProductStatusBadge node={node} />
      </button>
      {hasChildren && open && (
        <div>
          {(node.children ?? []).map((child) => (
            <TreeNode
              key={child.id}
              node={child}
              depth={depth + 1}
              selectedId={selectedId}
              onSelect={onSelect}
            />
          ))}
        </div>
      )}
    </div>
  );
}

export function ProductMapPage() {
  const { t } = useT("products");
  const wsId = useWorkspaceId();
  const { data, isLoading } = useQuery(productMapTreeOptions(wsId));
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const roots = useMemo(() => data?.nodes ?? [], [data]);
  const selected = useMemo(() => {
    if (!selectedId) return roots[0] ?? null;
    const walk = (nodes: ProductMapNode[]): ProductMapNode | null => {
      for (const n of nodes) {
        if (n.id === selectedId) return n;
        const found = walk(n.children ?? []);
        if (found) return found;
      }
      return null;
    };
    return walk(roots) ?? roots[0] ?? null;
  }, [roots, selectedId]);

  if (isLoading) {
    return (
      <div className="space-y-3 p-6">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-24 w-full" />
      </div>
    );
  }

  return (
    <div className="grid grid-cols-[minmax(280px,380px)_1fr] gap-4 p-6">
      <Card className="p-2">
        <div className="flex items-center gap-2 px-2 py-2 text-body font-semibold">
          <Network className="h-4 w-4" />
          {t(($) => $.tree_title)}
        </div>
        {roots.length === 0 ? (
          <p className="px-2 py-4 text-body text-muted-foreground">
            {t(($) => $.empty_tree)}
          </p>
        ) : (
          roots.map((node) => (
            <TreeNode
              key={node.id}
              node={node}
              depth={0}
              selectedId={selectedId}
              onSelect={setSelectedId}
            />
          ))
        )}
      </Card>
      <ProductNodeDetail node={selected} />
    </div>
  );
}
