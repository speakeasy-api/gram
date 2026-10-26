import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useMcpSlugValidation } from "./mcp-details-utils";

const { checkSlugAvailability } = vi.hoisted(() => ({
  checkSlugAvailability: vi.fn(),
}));

vi.mock("@/contexts/Sdk", () => {
  const client = { mcpEndpoints: { checkSlugAvailability } };
  return { useSdkClient: () => client };
});

beforeEach(() => {
  checkSlugAvailability.mockReset();
});

describe("useMcpSlugValidation", () => {
  it("reports a slug the endpoint check says is unavailable", async () => {
    checkSlugAvailability.mockResolvedValue(false);

    const { result } = renderHook(() =>
      useMcpSlugValidation("acme-taken", "acme-current", undefined),
    );

    await waitFor(() =>
      expect(result.current).toBe("This slug is already taken"),
    );
    expect(checkSlugAvailability).toHaveBeenCalledWith({
      slug: "acme-taken",
      customDomainId: undefined,
    });
  });

  it("accepts a slug the endpoint check says is available", async () => {
    checkSlugAvailability.mockResolvedValue(true);

    const { result } = renderHook(() =>
      useMcpSlugValidation("acme-free", "acme-current", undefined),
    );

    await waitFor(() => expect(checkSlugAvailability).toHaveBeenCalled());
    expect(result.current).toBeNull();
  });

  it("checks availability on the given custom domain", async () => {
    checkSlugAvailability.mockResolvedValue(true);

    renderHook(() => useMcpSlugValidation("tools", "current", "domain-1"));

    await waitFor(() =>
      expect(checkSlugAvailability).toHaveBeenCalledWith({
        slug: "tools",
        customDomainId: "domain-1",
      }),
    );
  });

  it("skips the check when the slug is unchanged", () => {
    const { result } = renderHook(() =>
      useMcpSlugValidation("acme-current", "acme-current", undefined),
    );

    expect(checkSlugAvailability).not.toHaveBeenCalled();
    expect(result.current).toBeNull();
  });

  it("reports no error when the check fails", async () => {
    checkSlugAvailability.mockRejectedValue(new Error("network down"));

    const { result } = renderHook(() =>
      useMcpSlugValidation("acme-new", "acme-current", undefined),
    );

    await waitFor(() => expect(checkSlugAvailability).toHaveBeenCalled());
    await Promise.resolve();
    expect(result.current).toBeNull();
  });

  it("reports format errors without checking availability", () => {
    const { result } = renderHook(() =>
      useMcpSlugValidation("Bad Slug", "acme-current", undefined),
    );

    expect(result.current).toBe("Lowercase letters, numbers, _ or - only");
    expect(checkSlugAvailability).not.toHaveBeenCalled();
  });
});
