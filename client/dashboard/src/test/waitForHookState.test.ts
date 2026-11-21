import { expect, it, vi } from "vitest";
import { waitForHookState } from "./waitForHookState";

it("waits until the asserted state transition occurs", async () => {
  let ready = false;
  let settled = false;
  const assertion = vi.fn(() => expect(ready).toBe(true));
  const waiting = waitForHookState(assertion).then(() => {
    settled = true;
  });
  await Promise.resolve();
  expect(assertion).toHaveBeenCalled();
  expect(settled).toBe(false);
  ready = true;
  await waiting;
  expect(settled).toBe(true);
});

it("rejects with the assertion failure when the condition never becomes true", async () => {
  await expect(
    waitForHookState(() => expect("pending").toBe("complete"), { timeout: 30 }),
  ).rejects.toThrow("complete");
});

it("finishes retrying after it times out", async () => {
  const assertion = vi.fn(() => {
    throw new Error("still pending");
  });
  await expect(waitForHookState(assertion, { timeout: 30 })).rejects.toThrow(
    "still pending",
  );
  const calls = assertion.mock.calls.length;
  await new Promise<void>((resolve) => {
    setTimeout(resolve, 20);
  });
  expect(assertion).toHaveBeenCalledTimes(calls);
});
