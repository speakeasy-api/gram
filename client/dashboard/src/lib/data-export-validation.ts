function hasInvalidURLControl(value: string): boolean {
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index);
    if (code <= 0x1f || code === 0x7f) return true;
  }
  return false;
}

export function isValidDataExportEndpointURL(raw: string): boolean {
  const value = raw.trim();
  if (
    value.includes("?") ||
    value.includes("#") ||
    hasInvalidURLControl(value)
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
