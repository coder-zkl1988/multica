"use client";

import { use } from "react";
import { InvestigationDetail } from "@multica/views/investigations";

export default function Page({ params }: { params: Promise<{ id: string }> }) {
  return <InvestigationDetail investigationId={use(params).id} />;
}
