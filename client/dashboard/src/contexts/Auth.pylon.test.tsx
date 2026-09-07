import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { clearLegacyUserStorage } from "@/lib/logout-storage";
import { initializePylon, PYLON_APP_ID } from "@/lib/pylon";
import { emptySession, usePylonInAppChat, type User } from "./Auth";

vi.mock("@/lib/pylon", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/pylon")>()),
  initializePylon: vi.fn(),
}));
vi.mock("@/lib/logout-storage", () => ({
  clearLegacyUserStorage: vi.fn(),
}));

const pylon = vi.fn<(action: string, ...args: unknown[]) => void>();
const signedInUser: User = {
  id: "user-test",
  email: "user@example.test",
  isAdmin: false,
  displayName: "Test User",
  photoUrl: "https://example.test/avatar.png",
  signature: "signed-email",
};

beforeEach(() => {
  vi.clearAllMocks();
  vi.stubEnv("PROD", true);
  window.Pylon = Object.assign(pylon, {
    q: [],
    e: vi.fn<(args: unknown) => void>(),
  });
});

afterEach(() => {
  cleanup();
  vi.unstubAllEnvs();
  Reflect.deleteProperty(window, "Pylon");
});

describe("usePylonInAppChat", () => {
  it.each([undefined, emptySession.user])(
    "does not initialize Pylon without an authenticated user (%j)",
    (user) => {
      renderHook(() => usePylonInAppChat(user));

      expect(clearLegacyUserStorage).toHaveBeenCalledOnce();
      expect(initializePylon).not.toHaveBeenCalled();
      expect(pylon).not.toHaveBeenCalled();
    },
  );

  it("identifies a signed-in user but does not reidentify after session expiry", () => {
    const { rerender } = renderHook((user) => usePylonInAppChat(user), {
      initialProps: emptySession.user,
    });
    expect(initializePylon).not.toHaveBeenCalled();

    rerender(signedInUser);
    expect(initializePylon).toHaveBeenCalledExactlyOnceWith({
      app_id: PYLON_APP_ID,
      email: signedInUser.email,
      name: signedInUser.displayName,
      avatar_url: signedInUser.photoUrl,
      email_hash: signedInUser.signature,
      hide_default_launcher: true,
    });
    expect(pylon).toHaveBeenCalledExactlyOnceWith("setNewIssueCustomFields", {
      gram: true,
    });

    rerender(emptySession.user);
    expect(initializePylon).toHaveBeenCalledOnce();
    expect(pylon).toHaveBeenCalledOnce();
  });

  it("does not initialize Pylon in development", () => {
    vi.stubEnv("PROD", false);
    renderHook(() => usePylonInAppChat(signedInUser));

    expect(initializePylon).not.toHaveBeenCalled();
    expect(pylon).not.toHaveBeenCalled();
  });
});
