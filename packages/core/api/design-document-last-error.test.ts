// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "./schema";
import { DesignDocumentSchema, EMPTY_DESIGN_DOCUMENT } from "./schemas";

// A design document's last_error is failure history, not a control surface:
// the server has written a bare string, the old flat object, and the current
// object with an optional diagnostics array. An installed desktop must render
// every one of them, so none of these shapes may fail the document parse.
describe("DesignDocumentSchema last_error", () => {
  const base = { ...EMPTY_DESIGN_DOCUMENT, id: "document-1", workspace_id: "ws-1", project_id: "project-1" };

  it("keeps the old flat shape without inventing diagnostics", () => {
    const parsed = DesignDocumentSchema.parse({
      ...base,
      last_error: { code: "design_document_cancelled", message: "design document task was cancelled", task_id: "task-1", operation: "generate" },
    });
    expect(parsed.last_error).toMatchObject({
      code: "design_document_cancelled",
      message: "design document task was cancelled",
      task_id: "task-1",
      operation: "generate",
      diagnostics: [],
    });
  });

  it("keeps every diagnostic the server recorded", () => {
    const parsed = DesignDocumentSchema.parse({
      ...base,
      last_error: {
        code: "design_document_audit_failed",
        message: "static audit failed",
        diagnostics: [
          { code: "prototype_inline_handler", path: "prototype/orders.html", message: "inline handler" },
          { code: "prototype_script_forbidden_api", path: "prototype/app.js", message: "network api" },
        ],
      },
    });
    expect(parsed.last_error).toMatchObject({
      code: "design_document_audit_failed",
      diagnostics: [
        { code: "prototype_inline_handler", path: "prototype/orders.html", message: "inline handler" },
        { code: "prototype_script_forbidden_api", path: "prototype/app.js", message: "network api" },
      ],
    });
  });

  it("degrades a malformed diagnostics array to empty instead of dropping the document", () => {
    for (const diagnostics of ["not an array", { code: "x" }, 7, null]) {
      const parsed = DesignDocumentSchema.parse({
        ...base,
        last_error: { code: "design_document_audit_failed", message: "static audit failed", diagnostics },
      });
      expect(parsed.last_error).toMatchObject({ code: "design_document_audit_failed", diagnostics: [] });
    }
    // An entry that is not an object is dropped; the readable siblings stay.
    const partial = DesignDocumentSchema.parse({
      ...base,
      last_error: {
        diagnostics: [
          { code: "prototype_inline_handler", path: "prototype/orders.html", message: "inline handler" },
          "nope",
          null,
        ],
      },
    });
    expect(partial.last_error).toMatchObject({
      diagnostics: [{ code: "prototype_inline_handler", path: "prototype/orders.html", message: "inline handler" }],
    });
  });

  it("keeps the two legacy scalar shapes", () => {
    expect(DesignDocumentSchema.parse({ ...base, last_error: "runtime went offline" }).last_error).toBe("runtime went offline");
    expect(DesignDocumentSchema.parse({ ...base, last_error: null }).last_error).toBeNull();
  });

  it("never falls back to the empty document over a malformed last_error", () => {
    const parsed = parseWithFallback(
      { ...base, last_error: 47 },
      DesignDocumentSchema,
      EMPTY_DESIGN_DOCUMENT,
      { endpoint: "GET /api/design-documents/:id" },
    );
    expect(parsed.id).toBe("document-1");
    expect(parsed.last_error).toBeNull();

    const fromNull = DesignDocumentSchema.parse({ ...base, last_error: undefined });
    expect(fromNull.id).toBe("document-1");
    expect(fromNull.last_error).toBeNull();
  });
});
