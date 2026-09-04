const INVALID_URL_CONTROL_PATTERN = /[\u0000-\u001f\u007f]/;

export function isValidDataExportEndpointURL(raw: string): boolean {
  const value = raw.trim();
  if (
    value.includes("?") ||
    value.includes("#") ||
    INVALID_URL_CONTROL_PATTERN.test(value)
  ) {
    return false;
  }

  try {
    const url = new URL(value);
    if (url.protocol !== "http:" && url.protocol !== "https:") return false;

    const schemePrefix = `${url.protocol}//`;
    if (
      value.slice(0, schemePrefix.length).toLowerCase() !== schemePrefix ||
      url.hostname === ""
    ) {
      return false;
    }

    const pathStart = value.indexOf("/", schemePrefix.length);
    const authority = value.slice(
      schemePrefix.length,
      pathStart === -1 ? undefined : pathStart,
    );
    return (
      authority !== "" && !authority.includes("@") && !authority.includes("\\")
    );
  } catch {
    return false;
  }
}
