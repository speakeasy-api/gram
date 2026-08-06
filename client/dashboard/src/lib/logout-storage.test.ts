// @vitest-environment happy-dom

import { beforeEach, describe, expect, it } from "vitest";

import { PREFERRED_THEME_STORAGE_KEY } from "./local-storage-keys";
import {
  clearLegacyUserStorage,
  clearStorageForLogout,
} from "./logout-storage";

function createStorage(): Storage {
  const items = new Map<string, string>();

  return {
    get length() {
      return items.size;
    },
    clear: () => items.clear(),
    getItem: (key: string) => items.get(key) ?? null,
    key: (index: number) => Array.from(items.keys())[index] ?? null,
    removeItem: (key: string) => {
      items.delete(key);
    },
    setItem: (key: string, value: string) => {
      items.set(key, value);
    },
  };
}

function blockStorageAccess(): void {
  for (const name of ["localStorage", "sessionStorage"] as const) {
    Object.defineProperty(window, name, {
      configurable: true,
      get: () => {
        throw new DOMException("Storage disabled", "SecurityError");
      },
    });
  }
}

describe("clearStorageForLogout", () => {
  beforeEach(() => {
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: createStorage(),
    });
    Object.defineProperty(window, "sessionStorage", {
      configurable: true,
      value: createStorage(),
    });
    window.localStorage.clear();
    window.sessionStorage.clear();
  });

  it("preserves theme and favorites while clearing user-scoped local storage", () => {
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "light");
    window.localStorage.setItem(
      "gram:org-favorites:<ORG_ID>",
      '["<PROJECT_ID>"]',
    );
    window.localStorage.setItem("gram:recents:<USER_ID>", '["/recent-page"]');
    window.localStorage.setItem("preferredProject", "project-slug");
    window.localStorage.setItem("pylon_user_email", "user@example.com");
    window.localStorage.setItem("pylon_user_display_name", "Example User");

    clearStorageForLogout();

    expect(window.localStorage.getItem(PREFERRED_THEME_STORAGE_KEY)).toBe(
      "light",
    );
    expect(window.localStorage.getItem("gram:org-favorites:<ORG_ID>")).toBe(
      '["<PROJECT_ID>"]',
    );
    expect(window.localStorage.getItem("gram:recents:<USER_ID>")).toBeNull();
    expect(window.localStorage.getItem("preferredProject")).toBeNull();
    expect(window.localStorage.getItem("pylon_user_email")).toBeNull();
    expect(window.localStorage.getItem("pylon_user_display_name")).toBeNull();
  });

  it("clears session storage", () => {
    window.sessionStorage.setItem("temporary", "value");

    clearStorageForLogout();

    expect(window.sessionStorage.getItem("temporary")).toBeNull();
  });

  it("removes legacy Pylon PII without clearing unrelated storage", () => {
    window.localStorage.setItem("pylon_user_email", "user@example.com");
    window.localStorage.setItem("pylon_user_display_name", "Example User");
    window.localStorage.setItem("unrelated", "value");
    window.sessionStorage.setItem("pylon_user_email", "user@example.com");

    clearLegacyUserStorage();

    expect(window.localStorage.getItem("pylon_user_email")).toBeNull();
    expect(window.localStorage.getItem("pylon_user_display_name")).toBeNull();
    expect(window.sessionStorage.getItem("pylon_user_email")).toBeNull();
    expect(window.localStorage.getItem("unrelated")).toBe("value");
  });

  it("degrades to a no-op when the browser blocks storage access", () => {
    blockStorageAccess();

    expect(() => clearStorageForLogout()).not.toThrow();
    expect(() => clearLegacyUserStorage()).not.toThrow();
  });
});
