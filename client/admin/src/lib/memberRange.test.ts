import {
  defaultParseSearch,
  defaultStringifySearch,
} from "@tanstack/react-router";
import { describe, expect, it } from "vitest";
import { memberRange, memberRangeErrors } from "./organizationFilters";

describe("member range", () => {
  it.each(["0", "5", "9007199254740993", "9223372036854775807"])(
    "round trips %s through the router without numeric coercion",
    (value) => {
      const parsed = defaultParseSearch(
        defaultStringifySearch({ minMembers: value, maxMembers: value }),
      );
      expect(memberRange(parsed)).toEqual({
        minMembers: value,
        maxMembers: value,
      });
      expect(memberRangeErrors(parsed)).toEqual({});
    },
  );
  it("canonicalizes whitespace and leading zeros, keeping either side optional", () => {
    expect(memberRange({ minMembers: " 0005 ", maxMembers: "" })).toEqual({
      minMembers: "5",
      maxMembers: undefined,
    });
    expect(memberRange({ maxMembers: "0" })).toEqual({
      minMembers: undefined,
      maxMembers: "0",
    });
    expect(memberRange({})).toEqual({
      minMembers: undefined,
      maxMembers: undefined,
    });
  });
  it.each([
    "-1",
    "1.5",
    "1e3",
    "+1",
    "abc",
    "5x",
    "9223372036854775808",
    [],
    {},
    true,
    0,
    9007199254740992,
  ])("drops invalid bound %j but retains its valid counterpart", (value) => {
    expect(memberRange({ minMembers: value, maxMembers: "10" })).toEqual({
      minMembers: undefined,
      maxMembers: "10",
    });
    expect(memberRangeErrors({ minMembers: value })).toHaveProperty(
      "minMembers",
    );
    expect(memberRange({ minMembers: "0", maxMembers: value })).toEqual({
      minMembers: "0",
      maxMembers: undefined,
    });
  });
  it("drops a reversed pair together, with a draft error", () => {
    const range = {
      minMembers: "9007199254740993",
      maxMembers: "9007199254740992",
    };
    expect(memberRange(range)).toEqual({
      minMembers: undefined,
      maxMembers: undefined,
    });
    expect(memberRangeErrors(range)).toHaveProperty("maxMembers");
  });
  it("rejects manual JSON numbers, including unsafe integers and exponents", () => {
    for (const value of [
      "9007199254740993",
      "9223372036854775807",
      "1e3",
      "1.0",
    ]) {
      expect(
        memberRange(defaultParseSearch(`?minMembers=${value}`)).minMembers,
      ).toBeUndefined();
    }
  });
});
