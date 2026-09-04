import { type Mock, vi } from "vitest";

let originalLocation: Location | undefined;

export function stubLocationReplace(): Mock {
  originalLocation = window.location;
  const replace = vi.fn();
  // @ts-expect-error happy-dom-compatible location replacement for redirect assertion
  delete window.location;
  Object.defineProperty(window, "location", {
    configurable: true,
    value: {
      // oxlint-disable-next-line typescript/no-misused-spread -- happy-dom Location is plain enough for tests
      ...originalLocation,
      replace,
    },
  });
  return replace;
}

export function restoreLocation(): void {
  if (!originalLocation) {
    return;
  }
  Object.defineProperty(window, "location", {
    configurable: true,
    value: originalLocation,
  });
  originalLocation = undefined;
}
