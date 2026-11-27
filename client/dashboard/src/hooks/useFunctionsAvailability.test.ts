import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import {
  useFunctionsAvailability,
  useFunctionsAvailable,
} from "./useFunctionsAvailability";

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
  [{ data: { functionsAvailable: true } }, "available", true],
  [{ data: { functionsAvailable: false } }, "unavailable", false],
  // A server that predates the setting always runs Functions.
  [{ data: {} }, "available", true],
  [{ data: undefined, isPending: true }, "loading", false],
  // Refused the organization's features: nothing is known about the
  // deployment, so Functions keep their usual gating.
  [{ isError: true, error: { statusCode: 403 } }, "available", true],
  [{ isError: true, error: { statusCode: 500 } }, "unavailable", false],
] as const)(
  "reads the deployment's Functions availability %j",
  (query, availability, available) => {
    mocks.features.mockReturnValue(query);
    expect(renderHook(() => useFunctionsAvailability()).result.current).toBe(
      availability,
    );
    expect(renderHook(() => useFunctionsAvailable()).result.current).toBe(
      available,
    );
  },
);
