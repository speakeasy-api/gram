export type ChangedField = {
  field: string;
  oldValue: unknown;
  newValue: unknown;
};

export function computeChangedFields(
  before: unknown,
  after: unknown,
): ChangedField[] {
  const beforeObj =
    before != null && typeof before === "object"
      ? (before as Record<string, unknown>)
      : {};
  const afterObj =
    after != null && typeof after === "object"
      ? (after as Record<string, unknown>)
      : {};

  const allKeys = new Set([
    ...Object.keys(beforeObj),
    ...Object.keys(afterObj),
  ]);

  const changes: ChangedField[] = [];

  for (const key of allKeys) {
    const oldVal = beforeObj[key];
    const newVal = afterObj[key];

    const oldStr = JSON.stringify(oldVal);
    const newStr = JSON.stringify(newVal);

    if (oldStr !== newStr) {
      changes.push({
        field: key,
        oldValue: oldVal,
        newValue: newVal,
      });
    }
  }

  return changes.sort((a, b) => a.field.localeCompare(b.field));
}
