export interface ChangelogEntry {
  id: string;
  title: string;
  description: string;
  date: string;
  type: "feature" | "improvement" | "fix" | "announcement";
  icon?: string;
}

export interface ChangelogResponse {
  entries: ChangelogEntry[];
  hasMore: boolean;
  nextCursor?: string;
}

// Mock data for development/fallback
const MOCK_CHANGELOG_DATA: ChangelogEntry[] = [
  {
    id: "dynamic-toolsets-v2",
    title: "Dynamic Toolsets v2 - Progressive Discovery",
    description: "Introducing a revolutionary approach that combines progressive discovery and semantic search capabilities. This new system exposes three core tools: search_tools, describe_tools, and execute_tool, enabling better scalability and natural language discovery while maintaining efficiency for large-scale deployments.",
    date: "2024-11-18",
    type: "feature",
  },
  {
    id: "gram-functions-composition",
    title: "Gram Functions Composition",
    description: "Gram instances are now fully composable, following patterns similar to Hono's grouping mechanism. This enhancement makes it possible to split, organize, and modularize Gram Functions code bases more effectively, enabling better code reusability and maintainability across complex projects.",
    date: "2024-11-18",
    type: "feature",
  },
  {
    id: "tool-selection-mode",
    title: "Tool Selection Mode Control",
    description: "New experimental feature providing granular control over how tools are presented to LLMs. Choose between Static mode (traditional MCP with all tools in context) or Dynamic mode (highly token-efficient approach optimized for large toolsets), with significant performance implications for context management.",
    date: "2024-11-18",
    type: "improvement",
  },
  {
    id: "semantic-search-tools",
    title: "Semantic Search for Tools",
    description: "Revolutionary semantic search capabilities that provide unparalleled efficiency and natural language discovery. This creates a system that scales naturally without sacrificing usability, allowing developers to find and execute tools using natural language queries rather than exact function names.",
    date: "2024-11-15",
    type: "feature",
  },
  {
    id: "gram-functions-launch",
    title: "Gram Functions Launch",
    description: "Build and deploy TypeScript tools as MCP servers on Gram's serverless infrastructure. Take full advantage of the NPM ecosystem, seamlessly wrap multiple APIs, query databases, and create powerful integrations - all with automatic scaling and zero infrastructure management.",
    date: "2024-11-10",
    type: "announcement",
  },
];

/**
 * Fetches the changelog entries for Gram
 * @param limit Number of entries to fetch
 * @param cursor Optional cursor for pagination
 * @returns Changelog response with entries
 */
export async function fetchChangelog(
  limit: number = 5,
  cursor?: string
): Promise<ChangelogResponse> {
  try {
    // In a production environment, you would either:
    // 1. Proxy through your backend to avoid CORS issues
    // 2. Have the changelog data served from your own API
    // 3. Use a CORS-enabled endpoint

    // For now, we'll try to fetch from a hypothetical endpoint
    // and fall back to mock data if it fails

    const params = new URLSearchParams({
      product: 'gram',
      limit: limit.toString(),
      ...(cursor && { cursor }),
    });

    // This would be the actual API call in production
    // const response = await fetch(`https://api.speakeasy.com/v1/changelog?${params}`);

    // For development, we'll simulate an API response
    const response = await simulateAPIResponse(limit, cursor);

    if (!response.ok) {
      throw new Error('Failed to fetch changelog');
    }

    const data = await response.json();
    return data;

  } catch (error) {
    console.warn('Failed to fetch changelog from API, using mock data:', error);

    // Return mock data as fallback
    const startIndex = cursor ? parseInt(cursor, 10) : 0;
    const entries = MOCK_CHANGELOG_DATA.slice(startIndex, startIndex + limit);

    return {
      entries,
      hasMore: startIndex + limit < MOCK_CHANGELOG_DATA.length,
      nextCursor: startIndex + limit < MOCK_CHANGELOG_DATA.length
        ? (startIndex + limit).toString()
        : undefined,
    };
  }
}

/**
 * Simulates an API response for development
 */
async function simulateAPIResponse(limit: number, cursor?: string): Promise<Response> {
  // Simulate network delay
  await new Promise(resolve => setTimeout(resolve, 500));

  const startIndex = cursor ? parseInt(cursor, 10) : 0;
  const entries = MOCK_CHANGELOG_DATA.slice(startIndex, startIndex + limit);

  const responseData: ChangelogResponse = {
    entries,
    hasMore: startIndex + limit < MOCK_CHANGELOG_DATA.length,
    nextCursor: startIndex + limit < MOCK_CHANGELOG_DATA.length
      ? (startIndex + limit).toString()
      : undefined,
  };

  // Simulate a successful response
  return new Response(JSON.stringify(responseData), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

/**
 * Formats a changelog entry for display
 */
export function formatChangelogEntry(entry: ChangelogEntry): {
  icon: string;
  badgeVariant: "success" | "information" | "warning" | "neutral";
  typeLabel: string;
} {
  switch (entry.type) {
    case "feature":
      return {
        icon: "sparkles",
        badgeVariant: "success",
        typeLabel: "New",
      };
    case "improvement":
      return {
        icon: "trending-up",
        badgeVariant: "information",
        typeLabel: "Improved",
      };
    case "fix":
      return {
        icon: "wrench",
        badgeVariant: "warning",
        typeLabel: "Fixed",
      };
    case "announcement":
      return {
        icon: "megaphone",
        badgeVariant: "neutral",
        typeLabel: "Announcement",
      };
    default:
      return {
        icon: "circle-dot",
        badgeVariant: "neutral",
        typeLabel: "Update",
      };
  }
}