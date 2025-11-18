import Anthropic from "@anthropic-ai/sdk";
import { MCPClient } from "./mcp-client.js";
import type {
  MCPServerConfig,
  TestConfig,
  TestPrompt,
  TestResult,
  TestSummary,
} from "./types.js";

/**
 * Main test runner for MCP server evaluation
 */
export class TestRunner {
  private anthropic: Anthropic;
  private config: TestConfig;

  constructor(config: TestConfig) {
    this.config = config;
    this.anthropic = new Anthropic({
      apiKey: config.anthropicApiKey,
    });
  }

  private fullResults: TestResult[] = []; // Store full results with logs

  /**
   * Run all tests across all MCP servers
   */
  async runAllTests(): Promise<TestSummary> {
    const results: TestResult[] = [];
    const numIterations = this.config.numIterations || 1;

    for (const mcpServer of this.config.mcpServers) {
      console.log(`\n=== Testing MCP Server: ${mcpServer.name} ===\n`);

      for (const prompt of this.config.prompts) {
        for (let iteration = 1; iteration <= numIterations; iteration++) {
          const iterationLabel =
            numIterations > 1
              ? ` (iteration ${iteration}/${numIterations})`
              : "";
          console.log(
            `Running test: ${prompt.description}${iterationLabel}...`,
          );

          const result = await this.runSingleTest(mcpServer, prompt, iteration);
          this.fullResults.push(result); // Store full result with logs
          results.push(result);

          if (result.success) {
            console.log(
              `✓ Success - Tokens: ${result.totalTokens} (${result.inputTokens} in, ${result.outputTokens} out), Tools: ${result.toolsUsedCount}, Duration: ${result.durationMs}ms`,
            );
          } else {
            console.log(`✗ Failed - ${result.error}`);
          }
        }
      }
    }

    return this.generateSummary(results);
  }

  /**
   * Run a single test with one MCP server and one prompt
   */
  private async runSingleTest(
    mcpServer: MCPServerConfig,
    prompt: TestPrompt,
    iteration: number,
  ): Promise<TestResult> {
    const startTime = Date.now();
    let mcpClient: MCPClient | null = null;

    try {
      // Connect to MCP server
      mcpClient = new MCPClient(mcpServer);
      await mcpClient.connect();

      // Get available tools
      const tools = await mcpClient.listTools();

      // Format tools for Anthropic API
      const anthropicTools = tools.map((tool: any) => ({
        name: tool.name,
        description: tool.description || "",
        input_schema: tool.inputSchema || { type: "object", properties: {} },
      }));

      // Initialize conversation messages
      const messages: Anthropic.MessageParam[] = [
        {
          role: "user",
          content: prompt.prompt,
        },
      ];

      // Track cumulative metrics across all turns
      let totalInputTokens = 0;
      let totalOutputTokens = 0;
      const allToolsCalled: string[] = [];
      const toolCallLogs: any[] = [];

      // Agentic loop: continue until Claude stops making tool calls
      const maxTurns = this.config.maxTurns || 20;
      let turnCount = 0;

      while (turnCount < maxTurns) {
        turnCount++;

        // Make request to Claude with tools
        const response = await this.anthropic.messages.create({
          model: this.config.model || "claude-3-5-sonnet-20241022",
          max_tokens: 4096,
          tools: anthropicTools,
          messages,
        });

        // Accumulate token usage
        totalInputTokens += response.usage.input_tokens;
        totalOutputTokens += response.usage.output_tokens;

        // Check if there are any tool uses in the response
        const toolUses = response.content.filter(
          (block: any) => block.type === "tool_use",
        );

        // If no tool uses, we're done
        if (toolUses.length === 0) {
          break;
        }

        // Track which tools were called
        toolUses.forEach((block: any) => allToolsCalled.push(block.name));

        // Add assistant's response to messages
        messages.push({
          role: "assistant",
          content: response.content,
        });

        // Execute tool calls and collect results
        const toolResults = await Promise.all(
          toolUses.map(async (toolUse: any) => {
            try {
              const result = await mcpClient!.callTool(
                toolUse.name,
                toolUse.input,
              );

              // Log the tool call details
              toolCallLogs.push({
                turnNumber: turnCount,
                toolName: toolUse.name,
                toolInput: toolUse.input,
                toolOutput: result,
                isError: false,
              });

              return {
                type: "tool_result" as const,
                tool_use_id: toolUse.id,
                content: JSON.stringify(result),
              };
            } catch (error) {
              const errorMessage =
                error instanceof Error ? error.message : String(error);

              // Log the tool call error
              toolCallLogs.push({
                turnNumber: turnCount,
                toolName: toolUse.name,
                toolInput: toolUse.input,
                toolOutput: { error: errorMessage },
                isError: true,
              });

              return {
                type: "tool_result" as const,
                tool_use_id: toolUse.id,
                content: JSON.stringify({
                  error: errorMessage,
                }),
                is_error: true,
              };
            }
          }),
        );

        // Add tool results to messages
        messages.push({
          role: "user",
          content: toolResults,
        });
      }

      const durationMs = Date.now() - startTime;

      return {
        promptId: prompt.id,
        promptDescription: prompt.description,
        mcpServerName: mcpServer.name,
        iteration,
        success: true,
        inputTokens: totalInputTokens,
        outputTokens: totalOutputTokens,
        totalTokens: totalInputTokens + totalOutputTokens,
        toolsUsedCount: allToolsCalled.length,
        toolsCalled: allToolsCalled,
        durationMs,
        model: this.config.model || "claude-3-5-sonnet-20241022",
        toolCallLogs,
        conversationLog: messages,
      };
    } catch (error) {
      const durationMs = Date.now() - startTime;
      return {
        promptId: prompt.id,
        promptDescription: prompt.description,
        mcpServerName: mcpServer.name,
        iteration,
        success: false,
        error: error instanceof Error ? error.message : String(error),
        inputTokens: 0,
        outputTokens: 0,
        totalTokens: 0,
        toolsUsedCount: 0,
        toolsCalled: [],
        durationMs,
        model: this.config.model || "claude-3-5-sonnet-20241022",
      };
    } finally {
      // Clean up MCP connection
      if (mcpClient) {
        await mcpClient.close();
      }
    }
  }

  /**
   * Generate summary statistics from test results
   */
  private generateSummary(results: TestResult[]): TestSummary {
    const totalTests = results.length;
    const successfulTests = results.filter((r) => r.success).length;
    const failedTests = totalTests - successfulTests;

    const totalInputTokens = results.reduce((sum, r) => sum + r.inputTokens, 0);
    const totalOutputTokens = results.reduce(
      (sum, r) => sum + r.outputTokens,
      0,
    );
    const totalTokens = totalInputTokens + totalOutputTokens;
    const totalToolCalls = results.reduce(
      (sum, r) => sum + r.toolsUsedCount,
      0,
    );
    const totalDurationMs = results.reduce((sum, r) => sum + r.durationMs, 0);

    // Strip out detailed logs from summary results
    const cleanResults = results.map(
      ({ toolCallLogs, conversationLog, ...rest }) => rest,
    );

    return {
      totalTests,
      successfulTests,
      failedTests,
      totalInputTokens,
      totalOutputTokens,
      totalTokens,
      totalToolCalls,
      totalDurationMs,
      results: cleanResults,
    };
  }

  /**
   * Print a formatted summary to console
   */
  printSummary(summary: TestSummary): void {
    console.log("\n" + "=".repeat(60));
    console.log("TEST SUMMARY");
    console.log("=".repeat(60));
    console.log(`Total Tests:        ${summary.totalTests}`);
    console.log(`Successful:         ${summary.successfulTests}`);
    console.log(`Failed:             ${summary.failedTests}`);
    console.log(`Total Tokens:       ${summary.totalTokens}`);
    console.log(`  - Input:          ${summary.totalInputTokens}`);
    console.log(`  - Output:         ${summary.totalOutputTokens}`);
    console.log(`Total Tool Calls:   ${summary.totalToolCalls}`);
    console.log(
      `Total Duration:     ${(summary.totalDurationMs / 1000).toFixed(2)}s`,
    );
    console.log("=".repeat(60));

    // Print per-server breakdown
    const serverStats = this.groupByServer(summary.results);
    console.log("\nPER-SERVER BREAKDOWN:");
    for (const [serverName, results] of Object.entries(serverStats)) {
      const serverTokens = results.reduce((sum, r) => sum + r.totalTokens, 0);
      const serverTools = results.reduce((sum, r) => sum + r.toolsUsedCount, 0);
      console.log(`\n${serverName}:`);
      console.log(`  Tests:       ${results.length}`);
      console.log(`  Tokens:      ${serverTokens}`);
      console.log(`  Tool Calls:  ${serverTools}`);
    }
  }

  /**
   * Group results by MCP server name
   */
  private groupByServer(results: TestResult[]): Record<string, TestResult[]> {
    return results.reduce(
      (acc, result) => {
        if (!acc[result.mcpServerName]) {
          acc[result.mcpServerName] = [];
        }
        acc[result.mcpServerName].push(result);
        return acc;
      },
      {} as Record<string, TestResult[]>,
    );
  }

  /**
   * Save results to JSON file
   */
  async saveResults(summary: TestSummary, filename: string): Promise<void> {
    const fs = await import("fs/promises");
    await fs.writeFile(filename, JSON.stringify(summary, null, 2));
    console.log(`\nResults saved to: ${filename}`);
  }

  /**
   * Save detailed logs to a separate JSON file
   */
  async saveDetailedLogs(
    summary: TestSummary,
    filename: string,
  ): Promise<void> {
    const fs = await import("fs/promises");

    // Create a detailed log structure using fullResults which have logs
    const detailedLogs = this.fullResults.map((result) => ({
      test: {
        promptId: result.promptId,
        promptDescription: result.promptDescription,
        mcpServerName: result.mcpServerName,
        iteration: result.iteration,
      },
      success: result.success,
      error: result.error,
      metrics: {
        inputTokens: result.inputTokens,
        outputTokens: result.outputTokens,
        totalTokens: result.totalTokens,
        toolsUsedCount: result.toolsUsedCount,
        toolsCalled: result.toolsCalled,
        durationMs: result.durationMs,
      },
      toolCallLogs: result.toolCallLogs || [],
      conversationLog: result.conversationLog || [],
    }));

    await fs.writeFile(filename, JSON.stringify(detailedLogs, null, 2));
    console.log(`Detailed logs saved to: ${filename}`);
  }
}
