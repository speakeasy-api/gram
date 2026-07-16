import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SkillTextDiff } from "./SkillTextDiff";

const testState = vi.hoisted(() => ({
  mobile: false,
  options: [] as unknown[],
  langs: [] as string[][],
}));

vi.mock("@/hooks/use-mobile", () => ({ useIsMobile: () => testState.mobile }));
vi.mock("@/components/diffs/provider", () => ({
  HighlightProvider: ({
    children,
    langs,
  }: {
    children: ReactNode;
    langs: string[];
  }) => {
    testState.langs.push(langs);
    return <div>{children}</div>;
  },
}));
vi.mock("@speakeasy-api/moonshine", () => ({
  useMoonshineConfig: () => ({ theme: "light" }),
}));
vi.mock("@pierre/diffs/react", () => ({
  MultiFileDiff: ({ options }: { options: unknown }) => {
    testState.options.push(options);
    return <div data-testid="diff" />;
  },
}));

afterEach(() => {
  cleanup();
  testState.options = [];
  testState.langs = [];
});

describe("SkillTextDiff", () => {
  it("renders exactly one split diff on desktop", () => {
    testState.mobile = false;
    render(
      <SkillTextDiff
        oldContent="old"
        newContent="new"
        oldLabel="a"
        newLabel="b"
      />,
    );
    expect(screen.getAllByTestId("diff")).toHaveLength(1);
    expect(testState.options).toEqual([
      expect.objectContaining({ diffStyle: "split" }),
    ]);
    expect(testState.langs).toEqual([["markdown"]]);
  });

  it("renders exactly one unified diff on mobile", () => {
    testState.mobile = true;
    render(
      <SkillTextDiff
        oldContent="old"
        newContent="new"
        oldLabel="a"
        newLabel="b"
      />,
    );
    expect(screen.getAllByTestId("diff")).toHaveLength(1);
    expect(testState.options).toEqual([
      expect.objectContaining({ diffStyle: "unified" }),
    ]);
  });
});
