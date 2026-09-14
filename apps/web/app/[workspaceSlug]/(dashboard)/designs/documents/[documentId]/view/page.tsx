"use client";

import { use } from "react";
import { DesignDocumentSavedPage } from "@multica/views/designs";

export default function Page({ params }: { params: Promise<{ documentId: string }> }) {
  const { documentId } = use(params);
  return <DesignDocumentSavedPage documentId={documentId} />;
}
