// The crumb watches this key; the entry itself holds only its raw JSON.
export function registryEntryNameKey(id: string): readonly [string, string] {
  return ["gram-admin-registry-entry-name", id] as const;
}

export function registryEntryName(dataJson: string): string | undefined {
  try {
    const name: unknown = JSON.parse(dataJson)?.server?.name;
    return typeof name === "string" && name !== "" ? name : undefined;
  } catch {
    return undefined;
  }
}
