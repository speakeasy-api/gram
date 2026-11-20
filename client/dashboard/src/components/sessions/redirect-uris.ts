/** The URIs worth sending: trimmed, with blank fields dropped. */
export function filledRedirectUris(value: string[]): string[] {
  return value.map((uri) => uri.trim()).filter((uri) => uri !== "");
}
