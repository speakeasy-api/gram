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

    expect(header.hasStoredValue).toBe(true);
    expect(preservesStoredHeaderValue(header)).toBe(true);
    expect(hasValidWriteOnlyHeaders([header])).toBe(true);
    expect(writeOnlyHeaderInput(header)).toEqual({ name: "Authorization" });
  });

  it("preserves an unchanged server header without a stored value", () => {
    const header = editableHeaderFromServer(
      { name: "Authorization", hasValue: false },
      0,
    );

    expect(header.hasStoredValue).toBe(false);
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

  it("rejects duplicate header names regardless of casing", () => {
    const first = blankWriteOnlyHeader();
    first.name = "Authorization";
    first.value = "Bearer first";
    const duplicate = blankWriteOnlyHeader();
    duplicate.name = " authorization ";
    duplicate.value = "Bearer second";

    expect(hasValidWriteOnlyHeaders([first, duplicate])).toBe(false);
  });

  it.each(["Bad Header", "Bad:Header", "Bad(Header)"])(
    "rejects invalid HTTP header name %s",
    (name) => {
      const header = blankWriteOnlyHeader();
      header.name = name;
      header.value = "value";

      expect(hasValidWriteOnlyHeaders([header])).toBe(false);
    },
  );

  it.each(["Bearer\nexample", "Bearer\u0000example", "Bearer\u007fexample"])(
    "rejects HTTP header values containing control characters",
    (value) => {
      const header = blankWriteOnlyHeader();
      header.name = "Authorization";
      header.value = value;

      expect(hasValidWriteOnlyHeaders([header])).toBe(false);
    },
  );

  it("allows horizontal tabs in HTTP header values", () => {
    const header = blankWriteOnlyHeader();
    header.name = "X-Trace-Context";
    header.value = "one\ttwo";

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
