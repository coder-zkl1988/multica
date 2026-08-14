"use client";

import type { ProductMapNode } from "@multica/core/products";
import { Card } from "@multica/ui/components/ui/card";
import { Badge } from "@multica/ui/components/ui/badge";
import { useT } from "../i18n";

import { ProductStatusBadge } from "./product-map-page";

export function ProductNodeDetail({ node }: { node: ProductMapNode | null }) {
  const { t } = useT("products");

  if (!node) {
    return (
      <Card className="p-6 text-body text-muted-foreground">
        {t(($) => $.select_node_hint)}
      </Card>
    );
  }

  const evidenceEntries = Object.entries(node.evidence ?? {});
  const statusSource =
    node.status_source === "pmo"
      ? t(($) => $.status_source.pmo)
      : t(($) => $.status_source.code_repo);

  return (
    <Card className="p-6">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h2 className="text-title font-semibold">{node.name}</h2>
          <p className="text-caption text-muted-foreground">
            {t(($) => $.meta_line, { slug: node.slug, source: statusSource })}
          </p>
        </div>
        <ProductStatusBadge node={node} />
      </div>

      {node.description && (
        <p className="mt-4 text-body text-muted-foreground">{node.description}</p>
      )}

      <div className="mt-6 grid grid-cols-2 gap-6">
        <div>
          <h3 className="text-body font-semibold">{t(($) => $.refs_title)}</h3>
          {node.refs.length === 0 ? (
            <p className="mt-2 text-body text-muted-foreground">
              {t(($) => $.refs_empty)}
            </p>
          ) : (
            <ul className="mt-2 space-y-1 text-body">
              {node.refs.map((ref) => (
                <li key={`${ref.ref_type}-${ref.ref_id}`} className="flex items-center gap-2">
                  <Badge variant="outline">{ref.ref_type}</Badge>
                  <code className="text-caption">{ref.ref_id}</code>
                </li>
              ))}
            </ul>
          )}
        </div>

        <div>
          <h3 className="text-body font-semibold">{t(($) => $.evidence_title)}</h3>
          {evidenceEntries.length > 0 ? (
            <dl className="mt-2 space-y-1 text-body">
              {evidenceEntries.map(([k, v]) => (
                <div key={k} className="flex gap-2">
                  <dt className="text-muted-foreground">{k}:</dt>
                  <dd className="truncate font-mono text-caption">
                    {typeof v === "string" ? v : JSON.stringify(v)}
                  </dd>
                </div>
              ))}
            </dl>
          ) : (
            <p className="mt-2 text-body text-muted-foreground">
              {t(($) => $.evidence_pending)}
            </p>
          )}
        </div>
      </div>

      <p className="mt-6 text-caption text-muted-foreground">
        {t(($) => $.updated_at, { time: new Date(node.updated_at).toLocaleString() })}
        {node.editors.length > 0 &&
          t(($) => $.editors_suffix, {
            ids: node.editors.map((e) => e.user_id).join(", "),
          })}
      </p>
    </Card>
  );
}
