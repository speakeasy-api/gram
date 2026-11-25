import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { useProjectSuspectedProblems, useSuspectedProblems } from "./api";

vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project-1", slug: "project" }),
  useSession: () => ({ session: "s" }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("./sdk", () => ({
  listProjectSuspectedProblems: () => new Promise(() => undefined),
  listSuspectedProblems: () => new Promise(() => undefined),
}));

describe("problem reads", () => {
  it("refetches on window focus only for the project-wide read", () => {
    const client = new QueryClient();
    renderHook(
      () => {
        useProjectSuspectedProblems({ limit: 6, enabled: true });
        useSuspectedProblems({ providerId: "iss-1" });
      },
      {
        wrapper: ({ children }: { children: ReactNode }) => (
          <QueryClientProvider client={client}>{children}</QueryClientProvider>
        ),
      },
    );
    const focus = (name: string) =>
      client
        .getQueryCache()
        .findAll()
        .find((q) => q.queryKey.includes(name))?.observers[0]?.options
        .refetchOnWindowFocus;
    expect(focus("listProjectSuspectedProblems")).toBe(true);
    expect(focus("listSuspectedProblems")).toBe(false);
  });
});
