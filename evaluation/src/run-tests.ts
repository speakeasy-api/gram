#!/usr/bin/env node
import { TestRunner } from "./test-runner.ts";
import type { TestConfig } from "./types.ts";
import * as fs from "fs/promises";
import * as path from "path";

/**
 * Generate timestamp for filenames (YYYY-MM-DD_HH-MM-SS)
 */
function getTimestamp(): string {
  const now = new Date();
  return now
    .toISOString()
    .replace(/T/, "_")
    .replace(/\..+/, "")
    .replace(/:/g, "-");
}

/**
 * Main entry point for running MCP evaluation tests
 */
async function main() {
  // Check for API key
  const apiKey = process.env.OPENROUTER_API_KEY;
  if (!apiKey) {
    console.error("Error: OPENROUTER_API_KEY environment variable is required");
    process.exit(1);
  }

  // Load config file (if provided)
  const configPath =
    process.argv[2] || path.join(process.cwd(), "test-config.json");

  let config: TestConfig;
  try {
    const configFile = await fs.readFile(configPath, "utf-8");
    const parsedConfig = JSON.parse(configFile);
    config = {
      ...parsedConfig,
      openrouterApiKey: apiKey,
    };
  } catch (error) {
    console.error(`Error loading config file: ${configPath}`);
    console.error(error);
    process.exit(1);
  }

  console.log("Starting MCP Server Evaluation Tests...");
  console.log(`Model: ${config.model || "claude-3-5-sonnet-20241022"}`);
  console.log(`MCP Servers: ${config.mcpServers.length}`);
  console.log(`Test Prompts: ${config.prompts.length}`);

  // Run tests
  const runner = new TestRunner(config);
  const summary = await runner.runAllTests();

  // Print summary
  runner.printSummary(summary);

  // Create timestamped results directory
  const timestamp = getTimestamp();
  const resultsDir = path.join(process.cwd(), "results", timestamp);
  await fs.mkdir(resultsDir, { recursive: true });

  const summaryFile = path.join(resultsDir, "summary.json");
  const detailedFile = path.join(resultsDir, "logs.json");

  // Save results
  await runner.saveResults(summary, summaryFile);
  await runner.saveDetailedLogs(summary, detailedFile);

  console.log(`\nResults saved to: results/${timestamp}/`);
  console.log(`  - summary.json`);
  console.log(`  - logs.json`);

  // Exit with error code if any tests failed
  if (summary.failedTests > 0) {
    process.exit(1);
  }
}

main().catch((error) => {
  console.error("Fatal error:", error);
  process.exit(1);
});
