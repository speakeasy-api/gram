// @vitest-environment node
import { describe, expect, it } from "vitest";

import {
  MAX_BULK_DISABLE_ORGANIZATIONS,
  parseBulkDisableCsv,
} from "./bulkDisableCsv";

describe("parseBulkDisableCsv", () => {
  it("reads an id column", () => {
    expect(parseBulkDisableCsv("id\norg_1\norg_2\n")).toEqual({
      ids: ["org_1", "org_2"],
      slugs: [],
    });
  });

  it("reads a slug column under a spreadsheet-style header", () => {
    expect(
      parseBulkDisableCsv("﻿Name,Organization Slug\r\nAcme,acme\r\n"),
    ).toEqual({ ids: [], slugs: ["acme"] });
  });

  it("prefers the id on a row and falls back to the slug", () => {
    expect(
      parseBulkDisableCsv("org_id,org_slug\norg_1,one\n,two\norg_3,\n"),
    ).toEqual({ ids: ["org_1", "org_3"], slugs: ["two"] });
  });

  it("trims and drops repeats", () => {
    expect(parseBulkDisableCsv('id\n org_1 \n"org_1"\norg_2\n')).toEqual({
      ids: ["org_1", "org_2"],
      slugs: [],
    });
  });

  it("refuses a file without an id or slug column", () => {
    expect(() => parseBulkDisableCsv("name,email\nAcme,a@b.c\n")).toThrow(
      'CSV needs an "id" or "slug" column in its first row. Found: name, email.',
    );
  });

  it("refuses an empty file and a header with no rows", () => {
    expect(() => parseBulkDisableCsv("")).toThrow("CSV is empty.");
    expect(() => parseBulkDisableCsv("id\n")).toThrow("CSV has no data rows.");
  });

  it("refuses a row that names no organization", () => {
    expect(() => parseBulkDisableCsv("id,name\norg_1,A\n,B\n")).toThrow(
      "Row 3 has no value for an id.",
    );
  });

  it("refuses two id columns", () => {
    expect(() => parseBulkDisableCsv("id,org_id\na,b\n")).toThrow(
      "CSV has more than one id column. Keep one.",
    );
  });

  it("refuses more organizations than one disable takes", () => {
    const rows = Array.from(
      { length: MAX_BULK_DISABLE_ORGANIZATIONS + 1 },
      (_, i) => `org_${i}`,
    );
    expect(() => parseBulkDisableCsv(["id", ...rows].join("\n"))).toThrow(
      /At most 1000 can be disabled at once/,
    );
  });

  it("refuses malformed quoting", () => {
    expect(() => parseBulkDisableCsv('id\n"org_1\n')).toThrow(
      "CSV contains an unclosed quoted field.",
    );
  });
});
