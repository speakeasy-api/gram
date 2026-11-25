import { createContext, use, useEffect } from "react";

/**
 * Lets a host learn what page a mini app is showing ("MCP servers", "Linear")
 * for breadcrumbs and for telling the assistant what the user is looking at.
 * Hosts without a breadcrumb (Storybook) leave the default no-op.
 */
const PageTitleContext = createContext<(title: string | null) => void>(
  () => {},
);

export const PageTitleProvider = PageTitleContext.Provider;

export function useReportPageTitle(title: string | null): void {
  const report = use(PageTitleContext);
  useEffect(() => {
    report(title);
  }, [report, title]);
}
