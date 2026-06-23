import type {
  HeaderInput,
  RemoteMcpServerHeader,
} from "@gram/client/models/components";

// Model + helpers for the remote MCP proxy-header editor. Kept separate from the
// HeadersEditor component so the component file only exports components (React
// fast-refresh constraint).

type HeaderMode = "static" | "passthrough";

export interface HeaderRow {
  key: string;
  name: string;
  mode: HeaderMode;
  value: string;
  valueFromRequestHeader: string;
  isSecret: boolean;
  isRequired: boolean;
  // True when loaded as a secret (its value comes back redacted). We then omit
  // the value on save unless the user types a replacement, so the stored
  // ciphertext is preserved rather than overwritten with the redaction.
  existingSecret: boolean;
  valueDirty: boolean;
}

let headerKeySeq = 0;
function nextHeaderKey(): string {
  headerKeySeq += 1;
  return `header-${headerKeySeq}`;
}

export function headerToRow(h: RemoteMcpServerHeader): HeaderRow {
  const passthrough = Boolean(h.valueFromRequestHeader);
  return {
    key: nextHeaderKey(),
    name: h.name,
    mode: passthrough ? "passthrough" : "static",
    value: h.isSecret ? "" : (h.value ?? ""),
    valueFromRequestHeader: h.valueFromRequestHeader ?? "",
    isSecret: h.isSecret,
    isRequired: h.isRequired,
    existingSecret: h.isSecret && !passthrough,
    valueDirty: false,
  };
}

export function emptyHeaderRow(): HeaderRow {
  return {
    key: nextHeaderKey(),
    name: "",
    mode: "static",
    value: "",
    valueFromRequestHeader: "",
    isSecret: false,
    isRequired: false,
    existingSecret: false,
    valueDirty: false,
  };
}

export function headerRowError(r: HeaderRow): string | null {
  if (!r.name.trim()) return "Header name is required";
  if (r.mode === "passthrough") {
    if (r.isSecret) {
      return "Secret headers cannot pass through a request header";
    }
    if (!r.valueFromRequestHeader.trim()) {
      return "Request header name is required";
    }
  } else if (!r.value.trim() && !(r.existingSecret && !r.valueDirty)) {
    return "Header value is required";
  }
  return null;
}

export function hasHeaderErrors(rows: HeaderRow[]): boolean {
  return rows.some((r) => headerRowError(r) !== null);
}

export function headerRowToInput(r: HeaderRow): HeaderInput {
  if (r.mode === "passthrough") {
    return {
      name: r.name.trim(),
      isRequired: r.isRequired,
      isSecret: false,
      valueFromRequestHeader: r.valueFromRequestHeader.trim(),
    };
  }
  const base: HeaderInput = {
    name: r.name.trim(),
    isRequired: r.isRequired,
    isSecret: r.isSecret,
  };
  // Preserve an unchanged secret by omitting value (server keeps ciphertext).
  if (r.isSecret && r.existingSecret && !r.valueDirty) {
    return base;
  }
  return { ...base, value: r.value };
}

// Signature for dirty-tracking: ignores the React key and the transient
// valueDirty flag so it reflects the persisted shape only.
export function headerRowsSignature(rows: HeaderRow[]): string {
  return JSON.stringify(
    rows.map((r) => ({
      name: r.name,
      mode: r.mode,
      value: r.value,
      valueFromRequestHeader: r.valueFromRequestHeader,
      isSecret: r.isSecret,
      isRequired: r.isRequired,
    })),
  );
}
