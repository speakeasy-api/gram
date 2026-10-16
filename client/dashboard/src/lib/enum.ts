/** Open response enums must be narrowed before using a closed request enum. */
export function isKnownEnumValue<T extends Record<string, string>>(
  values: T,
  value: string | undefined,
): value is T[keyof T] {
  return value !== undefined && Object.values(values).includes(value);
}

/** Use at mutation boundaries: never replace an unsupported value with a default. */
export function requireKnownEnumValue<T extends Record<string, string>>(
  values: T,
  value: string,
): T[keyof T] {
  if (isKnownEnumValue(values, value)) return value;
  throw new Error(
    "This value is not supported by this version of Speakeasy. Refresh the page before editing.",
  );
}

/** Unknown values (including inherited object keys) use an explicit presentation. */
export function enumPresentation<T extends object>(
  presentations: T,
  value: string,
  fallback: NoInfer<T[keyof T]>,
): T[keyof T] {
  return Object.hasOwn(presentations, value)
    ? (presentations[value as keyof T] ?? fallback)
    : fallback;
}
