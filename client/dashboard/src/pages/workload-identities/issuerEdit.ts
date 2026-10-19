import type { RegisterIssuerValues } from "./formValues";

/** The editable fields of a trusted platform that differ from what is stored. */
export interface IssuerChanges {
  name?: string;
  description?: string;
  jwksUri?: string;
  tags?: string[];
}

function sameTags(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((tag, index) => tag === b[index]);
}

/**
 * The fields an edit changes, compared as the server stores them (trimmed), so
 * that an edit sends only what the operator changed and leaves the rest
 * untouched. The issuer URL is not editable and is never included.
 */
export function changedIssuerFields(
  initial: RegisterIssuerValues,
  values: RegisterIssuerValues,
): IssuerChanges {
  const changes: IssuerChanges = {};

  const name = values.name.trim();
  if (name !== initial.name.trim()) {
    changes.name = name;
  }

  // Sent as an empty string to clear it: the server stores blank as none.
  const description = values.description.trim();
  if (description !== initial.description.trim()) {
    changes.description = description;
  }

  const jwksUri = values.jwksUri.trim();
  if (jwksUri !== initial.jwksUri.trim()) {
    changes.jwksUri = jwksUri;
  }

  if (!sameTags(values.tags, initial.tags)) {
    changes.tags = values.tags;
  }

  return changes;
}

export function issuerValuesDiffer(
  initial: RegisterIssuerValues,
  values: RegisterIssuerValues,
): boolean {
  return Object.keys(changedIssuerFields(initial, values)).length > 0;
}
