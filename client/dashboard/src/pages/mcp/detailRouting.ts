import type { useRoutes } from "@/routes";

type Routes = ReturnType<typeof useRoutes>;

export function decodePathSegment(segment: string): string {
  try {
    return decodeURIComponent(segment);
  } catch {
    return segment;
  }
}

export interface DetailRouting<Tab extends string> {
  /** Breadcrumb `skipSegments` source. */
  tabUrls: string[];
  isValidTab: (value: string) => value is Tab;
  /** The segment after the id, whether or not it names a tab. */
  tabSegmentFromPath: (pathname: string, id: string) => string | undefined;
  activeTabFromPath: (pathname: string, id: string) => Tab | undefined;
  tabHref: (routes: Routes, id: string, tab: Tab) => string;
}

/**
 * The tab routing an MCP detail page needs: which tabs exist, which one a
 * path names, and where each one lives. Servers and gateways differ only in
 * their tabs, where the id sits in the path, and their route table.
 */
export function createDetailRouting<const Tab extends string>({
  tabs,
  idSegmentIndex,
  tabRoutes,
}: {
  tabs: readonly Tab[];
  /** Index of the id among the decoded path segments, or -1 when absent. */
  idSegmentIndex: (segments: string[], id: string) => number;
  tabRoutes: (routes: Routes) => Record<Tab, { href: (id: string) => string }>;
}): DetailRouting<Tab> {
  function isValidTab(value: string): value is Tab {
    return (tabs as readonly string[]).includes(value);
  }

  /** The segment after the id, whether or not it names a tab. */
  function tabSegmentFromPath(
    pathname: string,
    id: string,
  ): string | undefined {
    if (!id) return undefined;
    const segments = pathname.split("/").filter(Boolean).map(decodePathSegment);
    const index = idSegmentIndex(segments, id);
    return index === -1 ? undefined : segments[index + 1];
  }

  function activeTabFromPath(pathname: string, id: string): Tab | undefined {
    const tabSegment = tabSegmentFromPath(pathname, id);
    return tabSegment && isValidTab(tabSegment) ? tabSegment : undefined;
  }

  function tabHref(routes: Routes, id: string, tab: Tab): string {
    return tabRoutes(routes)[tab].href(id);
  }

  return {
    // Breadcrumb `skipSegments` source. Derived from the tabs rather than
    // hand-maintained alongside them, so adding a tab can't leave a stray crumb.
    tabUrls: [...tabs] as string[],
    isValidTab,
    tabSegmentFromPath,
    activeTabFromPath,
    tabHref,
  };
}
