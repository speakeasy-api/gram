/**
 * Configuration for an MCP server to test
 */
export interface MCPServerConfig {
  name: string;
  command: string;
  args?: string[];
  env?: Record<string, string>;
}

/**
 * A test prompt to execute
 */
export interface TestPrompt {
  id: string;
  description: string;
  prompt: string;
}

/**
 * A single tool call in the conversation
 */
export interface ToolCallLog {
  turnNumber: number;
  toolName: string;
  toolInput: any;
  toolOutput: any;
  isError: boolean;
}

/**
 * Results from a single test execution
 */
export interface TestResult {
  promptId: string;
  promptDescription: string;
  mcpServerName: string;
  iteration: number; // Which iteration of this test (1-indexed)
  success: boolean;
  error?: string;
  inputTokens: number;
  outputTokens: number;
  totalTokens: number;
  toolsUsedCount: number;
  toolsCalled: string[];
  durationMs: number;
  model: string;
  toolCallLogs?: ToolCallLog[]; // Detailed logs of all tool calls
  conversationLog?: any[]; // Full conversation history
}

/**
 * Summary of all test results
 */
export interface TestSummary {
  totalTests: number;
  successfulTests: number;
  failedTests: number;
  totalInputTokens: number;
  totalOutputTokens: number;
  totalTokens: number;
  totalToolCalls: number;
  totalDurationMs: number;
  results: TestResult[];
}

/**
 * Configuration for the entire test suite
 */
export interface TestConfig {
  openrouterApiKey: string;
  model?: string; // Default: claude-3-5-sonnet-20241022
  numIterations?: number; // Default: 1 - number of times to run each prompt
  maxTurns?: number; // Default: 20 - maximum number of agentic turns per test
  mcpServers: MCPServerConfig[];
  prompts: TestPrompt[];
}
