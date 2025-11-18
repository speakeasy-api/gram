#!/usr/bin/env node
import { TestRunner } from './test-runner.js';
import type { TestConfig } from './types.js';
import * as fs from 'fs/promises';
import * as path from 'path';

/**
 * Main entry point for running MCP evaluation tests
 */
async function main() {
  // Check for API key
  const apiKey = process.env.ANTHROPIC_API_KEY;
  if (!apiKey) {
    console.error(
      'Error: ANTHROPIC_API_KEY environment variable is required'
    );
    process.exit(1);
  }

  // Load config file (if provided)
  const configPath =
    process.argv[2] || path.join(process.cwd(), 'test-config.json');

  let config: TestConfig;
  try {
    const configFile = await fs.readFile(configPath, 'utf-8');
    const parsedConfig = JSON.parse(configFile);
    config = {
      ...parsedConfig,
      anthropicApiKey: apiKey,
    };
  } catch (error) {
    console.error(`Error loading config file: ${configPath}`);
    console.error(error);
    process.exit(1);
  }

  console.log('Starting MCP Server Evaluation Tests...');
  console.log(`Model: ${config.model || 'claude-3-5-sonnet-20241022'}`);
  console.log(`MCP Servers: ${config.mcpServers.length}`);
  console.log(`Test Prompts: ${config.prompts.length}`);

  // Run tests
  const runner = new TestRunner(config);
  const summary = await runner.runAllTests();

  // Print summary
  runner.printSummary(summary);

  // Save results if configured
  if (config.outputFile) {
    await runner.saveResults(summary, config.outputFile);
  }

  // Save detailed logs if configured
  if (config.detailedLogsFile) {
    await runner.saveDetailedLogs(summary, config.detailedLogsFile);
  }

  // Exit with error code if any tests failed
  if (summary.failedTests > 0) {
    process.exit(1);
  }
}

main().catch((error) => {
  console.error('Fatal error:', error);
  process.exit(1);
});
