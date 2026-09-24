export type ValidationIssue = { path: string; message: string };

// Only the envelope is serialized. The record text is never rewritten.
export function validateRegistryText(
  dataJson: string,
  base?: { id: string; updatedAt: string },
): ValidationIssue[] {
  const bytes = new TextEncoder();
  if (bytes.encode(dataJson).length > 8 * 1024 * 1024)
    return [{ path: "/", message: "Record exceeds 8 MiB of UTF-8." }];
  const envelope = base
    ? { data_json: dataJson, id: base.id, updated_at: base.updatedAt }
    : { data_json: dataJson };
  if (bytes.encode(JSON.stringify(envelope)).length > 16 * 1024 * 1024)
    return [{ path: "/", message: "Request envelope exceeds 16 MiB." }];
  try {
    JSON.parse(dataJson);
    return [];
  } catch (error) {
    return [
      {
        path: "/",
        message: error instanceof Error ? error.message : "Invalid JSON",
      },
    ];
  }
}
