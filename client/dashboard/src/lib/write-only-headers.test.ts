import { describe, expect, it } from "vitest";
import {
  blankWriteOnlyHeader,
  editableHeaderFromServer,
  hasValidWriteOnlyHeaders,
  preservesStoredHeaderValue,
  writeOnlyHeaderInput,
} from "./write-only-headers";

describe("write-only header form helpers", () => {
  it("preserves an unchanged stored value without returning it", () => {
    const header = editableHeaderFromServer(
      { name: "Authorization", hasValue: true },
      0,
    );

    expect(preservesStoredHeaderValue(header)).toBe(true);
    expect(hasValidWriteOnlyHeaders([header])).toBe(true);
    expect(writeOnlyHeaderInput(header)).toEqual({ name: "Authorization" });
  });

  it("requires a replacement value when a stored header is renamed", () => {
    const header = {
      ...editableHeaderFromServer({ name: "Authorization", hasValue: true }, 0),
      name: "X-API-Key",
    };

    expect(preservesStoredHeaderValue(header)).toBe(false);
    expect(hasValidWriteOnlyHeaders([header])).toBe(false);
  });

  it("requires values for new headers", () => {
    const header = blankWriteOnlyHeader();
    header.name = "Authorization";

    expect(hasValidWriteOnlyHeaders([header])).toBe(false);
    header.value = "Bearer example";
    expect(hasValidWriteOnlyHeaders([header])).toBe(true);
  });

  it("trims submitted names and includes replacement values", () => {
    const header = blankWriteOnlyHeader();
    header.name = "  Authorization  ";
    header.value = "Bearer example";

    expect(writeOnlyHeaderInput(header)).toEqual({
      name: "Authorization",
      value: "Bearer example",
    });
  });
});
