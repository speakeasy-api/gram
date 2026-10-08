import { beforeEach, describe, expect, it, vi } from "vitest";

import { handleAPIError, handleError } from "@/lib/errors";

import {
  TOOLSET_CHANGED_MESSAGE,
  handleToolsetSaveError,
} from "./toolset-save-error";

vi.mock("@/lib/errors", () => ({
  handleError: vi.fn(),
  handleAPIError: vi.fn(),
}));

describe("handleToolsetSaveError", () => {
  beforeEach(() => {
    vi.mocked(handleError).mockClear();
    vi.mocked(handleAPIError).mockClear();
  });

  it("explains a stale-version conflict and offers a reload", () => {
    const conflict = Object.assign(
      new Error("expected_version_token is stale"),
      { statusCode: 409 },
    );
    const reload = vi.fn<() => void>();

    handleToolsetSaveError(conflict, reload);

    expect(handleAPIError).not.toHaveBeenCalled();
    expect(handleError).toHaveBeenCalledTimes(1);
    const [logged, options] = vi.mocked(handleError).mock.calls[0]!;
    expect(logged).toBe(conflict);
    expect(options?.message).toBe(TOOLSET_CHANGED_MESSAGE);
    expect(TOOLSET_CHANGED_MESSAGE).toMatch(/someone else changed/i);
    expect(options?.customAction?.label).toBe("Reload");
    options?.customAction?.onClick();
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it("keeps the server message for a 409 that is not a stale version", () => {
    const slugTaken = Object.assign(new Error("this slug is already taken"), {
      statusCode: 409,
    });

    handleToolsetSaveError(slugTaken, () => {});

    expect(handleError).not.toHaveBeenCalled();
    expect(handleAPIError).toHaveBeenCalledWith(slugTaken);
  });

  it("reports other failures through the shared API error handler", () => {
    const badRequest = Object.assign(new Error("invalid tool urn"), {
      statusCode: 400,
    });

    handleToolsetSaveError(badRequest, () => {});

    expect(handleError).not.toHaveBeenCalled();
    expect(handleAPIError).toHaveBeenCalledWith(badRequest);
  });
});
