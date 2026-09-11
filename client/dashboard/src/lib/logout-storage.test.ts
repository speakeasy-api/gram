// @vitest-environment happy-dom

import { beforeEach, describe, expect, it } from "vitest";

import {
  PRESERVED_LOCAL_STORAGE_KEYS,
  PRESERVED_LOCAL_STORAGE_PREFIXES,
  PREFERRED_THEME_STORAGE_KEY,
} from "./local-storage-keys";
import {
  LOGOUT_PRESERVE_WINDOW_NAME_PREFIX,
  capturePreservedStorage,
  capturePreservedStorageIfSafe,
  clearLegacyUserStorage,
  clearStorageForLogout,
  rememberPreservedStorageKey,
  resetPreservedStorageCapture,
  restorePreservedStorage,
  restorePreservedStorageBackup,
  setPreservedStorageImpersonating,
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
    window.name = "";
    resetPreservedStorageCapture();
    setPreservedStorageImpersonating(false);
  });

  it("keeps theme and favorites on the logout preserve lists", () => {
    expect(PRESERVED_LOCAL_STORAGE_KEYS).toContain(PREFERRED_THEME_STORAGE_KEY);
    expect(PRESERVED_LOCAL_STORAGE_PREFIXES).toContain("gram:org-favorites:");
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

  // The logout response carries Clear-Site-Data, so the browser empties
  // localStorage before any response handler runs. Capture/restore is what
  // survives that wipe.
  it("restores captured entries into storage the browser already emptied", () => {
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "dark");
    window.localStorage.setItem(
      "gram:org-favorites:<ORG_ID>",
      '["<PROJECT_ID>"]',
    );
    window.localStorage.setItem("preferredProject", "project-slug");

    const preserved = capturePreservedStorage();
    window.localStorage.clear();
    restorePreservedStorage(preserved);

    expect(window.localStorage.getItem(PREFERRED_THEME_STORAGE_KEY)).toBe(
      "dark",
    );
    expect(window.localStorage.getItem("gram:org-favorites:<ORG_ID>")).toBe(
      '["<PROJECT_ID>"]',
    );
    expect(window.localStorage.getItem("preferredProject")).toBeNull();
  });

  // The logout response hook clears a store the browser may already have
  // emptied, so the entries it keeps have to come from the snapshot rather than
  // from a re-read of storage.
  it("keeps entries from a supplied snapshot when storage is already empty", () => {
    const preserved = [
      [PREFERRED_THEME_STORAGE_KEY, "dark"],
      ["gram:org-favorites:<ORG_ID>", '["<PROJECT_ID>"]'],
    ] as const;
    window.localStorage.setItem("preferredProject", "project-slug");
    window.localStorage.clear();

    clearStorageForLogout(preserved);

    expect(window.localStorage.getItem(PREFERRED_THEME_STORAGE_KEY)).toBe(
      "dark",
    );
    expect(window.localStorage.getItem("gram:org-favorites:<ORG_ID>")).toBe(
      '["<PROJECT_ID>"]',
    );
    expect(window.localStorage.getItem("preferredProject")).toBeNull();
  });

  it("captures nothing but the preserved keys", () => {
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "dark");
    window.localStorage.setItem("pylon_user_email", "user@example.com");

    expect(capturePreservedStorage()).toEqual([
      [PREFERRED_THEME_STORAGE_KEY, "dark"],
    ]);
  });

  it("degrades to a no-op when storage is blocked around a logout", () => {
    blockStorageAccess();

    expect(capturePreservedStorage()).toEqual([]);
    expect(() =>
      restorePreservedStorage([[PREFERRED_THEME_STORAGE_KEY, "dark"]]),
    ).not.toThrow();
  });

  // Impersonation exit and "switch account" call logout, then navigate to
  // /login. The response hook may never run (timeout, or the Request identity
  // used as the WeakMap key does not match). The capture taken before the
  // request is what has to refill the emptied store.
  it("restores from the last capture when logout is given no snapshot", () => {
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "dark");
    window.localStorage.setItem(
      "gram:org-favorites:<ORG_ID>",
      '["<PROJECT_ID>"]',
    );
    window.localStorage.setItem("preferredProject", "project-slug");

    capturePreservedStorage();
    window.localStorage.clear();

    clearStorageForLogout();

    expect(window.localStorage.getItem(PREFERRED_THEME_STORAGE_KEY)).toBe(
      "dark",
    );
    expect(window.localStorage.getItem("gram:org-favorites:<ORG_ID>")).toBe(
      '["<PROJECT_ID>"]',
    );
    expect(window.localStorage.getItem("preferredProject")).toBeNull();
  });

  it("survives a post-wipe navigation via the window.name backup", () => {
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "dark");
    window.localStorage.setItem(
      "gram:org-favorites:<ORG_ID>",
      '["<PROJECT_ID>"]',
    );
    window.localStorage.setItem("preferredProject", "project-slug");

    capturePreservedStorage();
    expect(window.name.startsWith(LOGOUT_PRESERVE_WINDOW_NAME_PREFIX)).toBe(
      true,
    );

    window.localStorage.clear();
    restorePreservedStorageBackup();

    expect(window.localStorage.getItem(PREFERRED_THEME_STORAGE_KEY)).toBe(
      "dark",
    );
    expect(window.localStorage.getItem("gram:org-favorites:<ORG_ID>")).toBe(
      '["<PROJECT_ID>"]',
    );
    expect(window.localStorage.getItem("preferredProject")).toBeNull();
    expect(window.name.startsWith(LOGOUT_PRESERVE_WINDOW_NAME_PREFIX)).toBe(
      true,
    );
  });

  it("ignores non-preserved keys smuggled in the window.name backup", () => {
    window.name = `${LOGOUT_PRESERVE_WINDOW_NAME_PREFIX}${JSON.stringify([
      [PREFERRED_THEME_STORAGE_KEY, "dark"],
      ["pylon_user_email", "user@example.com"],
    ])}`;

    restorePreservedStorageBackup();

    expect(window.localStorage.getItem(PREFERRED_THEME_STORAGE_KEY)).toBe(
      "dark",
    );
    expect(window.localStorage.getItem("pylon_user_email")).toBeNull();
  });

  it("does not overwrite an unrelated window.name", () => {
    window.name = "other-tab-state";
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "dark");

    capturePreservedStorage();

    expect(window.name).toBe("other-tab-state");
  });

  // Platform admins snapshot in their own org. Impersonation must not replace
  // that snapshot with the customer org's theme or favorites.
  it("does not refresh the snapshot while impersonating", () => {
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "dark");
    window.localStorage.setItem(
      "gram:org-favorites:<ADMIN_ORG_ID>",
      '["<ADMIN_PROJECT_ID>"]',
    );
    capturePreservedStorage();

    setPreservedStorageImpersonating(true);
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "light");
    window.localStorage.setItem(
      "gram:org-favorites:<CUSTOMER_ORG_ID>",
      '["<CUSTOMER_PROJECT_ID>"]',
    );

    expect(capturePreservedStorageIfSafe()).toEqual([
      [PREFERRED_THEME_STORAGE_KEY, "dark"],
      ["gram:org-favorites:<ADMIN_ORG_ID>", '["<ADMIN_PROJECT_ID>"]'],
    ]);

    window.localStorage.clear();
    clearStorageForLogout(capturePreservedStorageIfSafe());

    expect(window.localStorage.getItem(PREFERRED_THEME_STORAGE_KEY)).toBe(
      "dark",
    );
    expect(
      window.localStorage.getItem("gram:org-favorites:<ADMIN_ORG_ID>"),
    ).toBe('["<ADMIN_PROJECT_ID>"]');
    expect(
      window.localStorage.getItem("gram:org-favorites:<CUSTOMER_ORG_ID>"),
    ).toBeNull();
  });

  it("seals the live store on the rising edge of impersonation", () => {
    resetPreservedStorageCapture();
    window.name = "";
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "dark");
    window.localStorage.setItem(
      "gram:org-favorites:<ADMIN_ORG_ID>",
      '["<ADMIN_PROJECT_ID>"]',
    );

    setPreservedStorageImpersonating(true);
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "light");
    window.localStorage.setItem(
      "gram:org-favorites:<CUSTOMER_ORG_ID>",
      '["<CUSTOMER_PROJECT_ID>"]',
    );

    clearStorageForLogout(capturePreservedStorageIfSafe());

    expect(window.localStorage.getItem(PREFERRED_THEME_STORAGE_KEY)).toBe(
      "dark",
    );
    expect(
      window.localStorage.getItem("gram:org-favorites:<ADMIN_ORG_ID>"),
    ).toBe('["<ADMIN_PROJECT_ID>"]');
    expect(
      window.localStorage.getItem("gram:org-favorites:<CUSTOMER_ORG_ID>"),
    ).toBeNull();
  });

  it("restores the last snapshot on no-arg cleanup while impersonating", () => {
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "dark");
    capturePreservedStorage();
    setPreservedStorageImpersonating(true);
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "light");
    window.localStorage.setItem(
      "gram:org-favorites:<CUSTOMER_ORG_ID>",
      '["<CUSTOMER_PROJECT_ID>"]',
    );

    clearStorageForLogout();

    expect(window.localStorage.getItem(PREFERRED_THEME_STORAGE_KEY)).toBe(
      "dark",
    );
    expect(
      window.localStorage.getItem("gram:org-favorites:<CUSTOMER_ORG_ID>"),
    ).toBeNull();
  });

  it("upserts one preserved key without scanning the rest of the store", () => {
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "dark");
    capturePreservedStorage();
    window.localStorage.setItem(
      "gram:org-favorites:<CUSTOMER_ORG_ID>",
      '["<CUSTOMER_PROJECT_ID>"]',
    );

    rememberPreservedStorageKey(PREFERRED_THEME_STORAGE_KEY, "light");
    clearStorageForLogout();

    expect(window.localStorage.getItem(PREFERRED_THEME_STORAGE_KEY)).toBe(
      "light",
    );
    expect(
      window.localStorage.getItem("gram:org-favorites:<CUSTOMER_ORG_ID>"),
    ).toBeNull();
  });

  it("does not recapture on impersonation when the sealed snapshot is empty", () => {
    expect(capturePreservedStorage()).toEqual([]);
    expect(window.name.startsWith(LOGOUT_PRESERVE_WINDOW_NAME_PREFIX)).toBe(
      true,
    );
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "light");
    window.localStorage.setItem(
      "gram:org-favorites:<CUSTOMER_ORG_ID>",
      '["<CUSTOMER_PROJECT_ID>"]',
    );

    setPreservedStorageImpersonating(true);
    clearStorageForLogout(capturePreservedStorageIfSafe());

    expect(window.localStorage.getItem(PREFERRED_THEME_STORAGE_KEY)).toBeNull();
    expect(
      window.localStorage.getItem("gram:org-favorites:<CUSTOMER_ORG_ID>"),
    ).toBeNull();
  });

  it("does not upsert preserved keys before the session is classified", () => {
    resetPreservedStorageCapture();
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "dark");
    capturePreservedStorage();

    rememberPreservedStorageKey(PREFERRED_THEME_STORAGE_KEY, "light");
    setPreservedStorageImpersonating(true);
    clearStorageForLogout(capturePreservedStorageIfSafe());

    expect(window.localStorage.getItem(PREFERRED_THEME_STORAGE_KEY)).toBe(
      "dark",
    );
  });

  it("does not upsert while impersonating", () => {
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "dark");
    capturePreservedStorage();
    setPreservedStorageImpersonating(true);

    rememberPreservedStorageKey(PREFERRED_THEME_STORAGE_KEY, "light");

    expect(capturePreservedStorageIfSafe()).toEqual([
      [PREFERRED_THEME_STORAGE_KEY, "dark"],
    ]);
  });

  it("refreshes the snapshot on a normal admin session", () => {
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "dark");
    capturePreservedStorage();

    setPreservedStorageImpersonating(false);
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "light");

    expect(capturePreservedStorageIfSafe()).toEqual([
      [PREFERRED_THEME_STORAGE_KEY, "light"],
    ]);
  });

  it("restores the pre-impersonation snapshot after a new-document load", () => {
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "dark");
    window.localStorage.setItem(
      "gram:org-favorites:<ADMIN_ORG_ID>",
      '["<ADMIN_PROJECT_ID>"]',
    );
    capturePreservedStorage();

    // Support-session start is a full navigation: heap is gone, backup remains.
    window.localStorage.clear();
    resetPreservedStorageCapture();
    restorePreservedStorageBackup();
    setPreservedStorageImpersonating(true);
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "light");
    window.localStorage.setItem(
      "gram:org-favorites:<CUSTOMER_ORG_ID>",
      '["<CUSTOMER_PROJECT_ID>"]',
    );

    window.localStorage.clear();
    clearStorageForLogout(capturePreservedStorageIfSafe());

    expect(window.localStorage.getItem(PREFERRED_THEME_STORAGE_KEY)).toBe(
      "dark",
    );
    expect(
      window.localStorage.getItem("gram:org-favorites:<ADMIN_ORG_ID>"),
    ).toBe('["<ADMIN_PROJECT_ID>"]');
    expect(
      window.localStorage.getItem("gram:org-favorites:<CUSTOMER_ORG_ID>"),
    ).toBeNull();
  });

  it("does not live-capture impersonated storage when no snapshot exists", () => {
    setPreservedStorageImpersonating(true);
    window.localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, "light");
    window.localStorage.setItem(
      "gram:org-favorites:<CUSTOMER_ORG_ID>",
      '["<CUSTOMER_PROJECT_ID>"]',
    );

    clearStorageForLogout();

    expect(window.localStorage.getItem(PREFERRED_THEME_STORAGE_KEY)).toBeNull();
    expect(
      window.localStorage.getItem("gram:org-favorites:<CUSTOMER_ORG_ID>"),
    ).toBeNull();
  });
});
