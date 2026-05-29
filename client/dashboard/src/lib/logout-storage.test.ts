// @vitest-environment happy-dom

import { beforeEach, describe, expect, it } from "vitest";

import {
  PREFERRED_THEME_STORAGE_KEY,
  PROJECT_FAVORITES_STORAGE_PREFIX,
} from "./local-storage-keys";
import { clearStorageForLogout } from "./logout-storage";

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

  it("preserves preferred theme and organization favorites while clearing other local storage", () => {
    const favoritesKey = `${PROJECT_FAVORITES_STORAGE_PREFIX}org_123`;

    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "light");
    window.localStorage.setItem(favoritesKey, '["project_123"]');
    window.localStorage.setItem("preferredProject", "project-slug");
    window.localStorage.setItem("pylon_user_email", "user@example.com");

    clearStorageForLogout();

    expect(window.localStorage.getItem(PREFERRED_THEME_STORAGE_KEY)).toBe(
      "light",
    );
    expect(window.localStorage.getItem(favoritesKey)).toBe('["project_123"]');
    expect(window.localStorage.getItem("preferredProject")).toBeNull();
    expect(window.localStorage.getItem("pylon_user_email")).toBeNull();
  });

  it("clears session storage", () => {
    window.sessionStorage.setItem("temporary", "value");

    clearStorageForLogout();

    expect(window.sessionStorage.getItem("temporary")).toBeNull();
  });
});
