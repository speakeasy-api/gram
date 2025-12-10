/**
 * Detects if WebGL is available in the current browser environment.
 * Returns true if WebGL can be used, false otherwise.
 */
export function isWebGLAvailable(): boolean {
  try {
    const canvas = document.createElement("canvas");
    const gl =
      canvas.getContext("webgl2") ||
      canvas.getContext("webgl") ||
      canvas.getContext("experimental-webgl");
    return gl !== null;
  } catch {
    return false;
  }
}

/**
 * Cached result of WebGL availability check.
 * Computed once on first access.
 */
let cachedResult: boolean | null = null;

export function getWebGLAvailability(): boolean {
  if (cachedResult === null) {
    cachedResult = isWebGLAvailable();
  }
  return cachedResult;
}
