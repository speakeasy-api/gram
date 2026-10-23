/** Never advertise a plaintext bearer-token destination outside local development. */
export function inferenceCompletionEndpoint(
  base: string,
  localDevelopment: boolean,
): string | null {
  try {
    const url = new URL(base);
    const localHTTP =
      localDevelopment &&
      url.protocol === "http:" &&
      ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname);
    if (
      (url.protocol !== "https:" && !localHTTP) ||
      url.username ||
      url.password
    )
      return null;
    return new URL("/chat/completions", url).toString();
  } catch {
    return null;
  }
}
