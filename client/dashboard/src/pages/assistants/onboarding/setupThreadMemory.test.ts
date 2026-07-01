// @vitest-environment happy-dom

import { beforeEach, describe, expect, it } from "vitest";

import { ASSISTANT_SETUP_THREAD_STORAGE_PREFIX } from "@/lib/local-storage-keys";
import {
  readStoredSetupThreadId,
  writeStoredSetupThreadId,
} from "./setupThreadMemory";

describe("setupThreadMemory", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("round-trips a thread id for a project/user/assistant tuple", () => {
    writeStoredSetupThreadId("proj_1", "user_1", "asst_1", "thread_abc");

    expect(readStoredSetupThreadId("proj_1", "user_1", "asst_1")).toBe(
      "thread_abc",
    );
  });

  it("stores under the shared assistant-setup-thread prefix so logout preservation applies", () => {
    writeStoredSetupThreadId("proj_1", "user_1", "asst_1", "thread_abc");

    expect(
      window.localStorage.getItem(
        `${ASSISTANT_SETUP_THREAD_STORAGE_PREFIX}proj_1:user_1:asst_1`,
      ),
    ).toBe("thread_abc");
  });

  it("returns undefined when nothing was stored", () => {
    expect(
      readStoredSetupThreadId("proj_1", "user_1", "asst_1"),
    ).toBeUndefined();
  });

  it("scopes threads per project, user, and assistant", () => {
    writeStoredSetupThreadId("proj_1", "user_1", "asst_1", "thread_abc");

    expect(
      readStoredSetupThreadId("proj_2", "user_1", "asst_1"),
    ).toBeUndefined();
    expect(
      readStoredSetupThreadId("proj_1", "user_2", "asst_1"),
    ).toBeUndefined();
    expect(
      readStoredSetupThreadId("proj_1", "user_1", "asst_2"),
    ).toBeUndefined();
  });

  it("overwrites with the latest thread for the same assistant", () => {
    writeStoredSetupThreadId("proj_1", "user_1", "asst_1", "thread_old");
    writeStoredSetupThreadId("proj_1", "user_1", "asst_1", "thread_new");

    expect(readStoredSetupThreadId("proj_1", "user_1", "asst_1")).toBe(
      "thread_new",
    );
  });

  it("degrades to a no-op when storage is unavailable", () => {
    const original = window.localStorage;
    const throwingStorage: Pick<Storage, "getItem" | "setItem"> = {
      getItem: () => {
        throw new Error("storage disabled");
      },
      setItem: () => {
        throw new Error("storage disabled");
      },
    };
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: throwingStorage,
    });

    try {
      expect(() =>
        writeStoredSetupThreadId("proj_1", "user_1", "asst_1", "thread_abc"),
      ).not.toThrow();
      expect(
        readStoredSetupThreadId("proj_1", "user_1", "asst_1"),
      ).toBeUndefined();
    } finally {
      Object.defineProperty(window, "localStorage", {
        configurable: true,
        value: original,
      });
    }
  });
});
