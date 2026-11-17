import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useFunctionsAvailable } from "./useFunctionsAvailable";

const mocks = vi.hoisted(() => ({
  features: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-one" }),
}));
vi.mock("@gram/client/react-query/productFeatures.js", () => ({
  useProductFeatures: mocks.features,
}));
afterEach(cleanup);
it.each([
  [{ data: { functionsAvailable: true } }, "available"],
  [{ data: { functionsAvailable: false } }, "unavailable"],
  // An older server omits the field; Functions stay hidden.
  [{ data: {} }, "unavailable"],
  [{ data: undefined, isError: true }, "unavailable"],
  [{ data: undefined, isPending: true }, "loading"],
] as const)("reports Functions availability for %j", (query, expected) => {
  mocks.features.mockReturnValue(query);
  const { result } = renderHook(() => useFunctionsAvailable());
  expect(result.current.status).toBe(expected);
  expect(result.current.available).toBe(expected === "available");
});
