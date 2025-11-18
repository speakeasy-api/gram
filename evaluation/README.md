# MCP Server Evaluation

Performance testing framework for evaluating MCP servers with LLMs. This tool connects to MCP servers, executes test prompts using Claude, and records token usage and tool call metrics.

## Features

- Connect to multiple MCP servers via stdio
- Execute configurable test prompts
- Track token usage (input, output, total)
- Count tool calls and identify which tools were used
- Measure execution duration
- Generate summary statistics
- Export results to JSON

## Installation

```bash
pnpm install
```

## Usage

### 1. Set up your Anthropic API key

```bash
export ANTHROPIC_API_KEY="your-api-key"
```

### 2. Create a test configuration file

Copy the example config:

```bash
cp test-config.example.json test-config.json
```

Edit `test-config.json` to configure:
- **model**: Claude model to use (default: `claude-3-5-sonnet-20241022`)
- **numIterations**: Number of times to run each prompt (default: `1`)
- **mcpServers**: Array of MCP servers to test
  - `name`: Friendly name for the server
  - `command`: Command to run the MCP server
  - `args`: Command arguments
  - `env`: Environment variables (e.g., API keys)
- **prompts**: Array of test prompts
  - `id`: Unique identifier
  - `description`: Human-readable description
  - `prompt`: The actual prompt to send to Claude
  - `expectedTools`: (optional) List of tools you expect to be called
- **outputFile**: (optional) Path to save JSON results

### 3. Run the tests

```bash
pnpm test
```

Or with a custom config file:

```bash
pnpm test path/to/your-config.json
```

## Example Configuration

```json
{
  "model": "claude-3-5-sonnet-20241022",
  "numIterations": 10,
  "mcpServers": [
    {
      "name": "filesystem",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
      "env": {}
    }
  ],
  "prompts": [
    {
      "id": "list-files",
      "description": "List files in directory",
      "prompt": "List all files in the current directory"
    }
  ],
  "outputFile": "test-results.json"
}
```

## Output

The tool provides:

1. **Real-time console output**: Shows progress and results for each test
2. **Summary statistics**: Aggregated metrics across all tests
3. **Per-server breakdown**: Token usage and tool calls grouped by MCP server
4. **JSON export**: Detailed results saved to the configured output file

### Example Output

```
=== Testing MCP Server: filesystem ===

Running test: List files in directory (iteration 1/10)...
✓ Success - Tokens: 523 (450 in, 73 out), Tools: 1, Duration: 1234ms
Running test: List files in directory (iteration 2/10)...
✓ Success - Tokens: 525 (452 in, 73 out), Tools: 1, Duration: 1198ms
...

============================================================
TEST SUMMARY
============================================================
Total Tests:        30
Successful:         30
Failed:             0
Total Tokens:       15230
  - Input:          12500
  - Output:         2730
Total Tool Calls:   50
Total Duration:     34.56s
============================================================

PER-SERVER BREAKDOWN:

filesystem:
  Tests:       20
  Tokens:      10430
  Tool Calls:  30

github:
  Tests:       10
  Tokens:      4800
  Tool Calls:  20
```

## Test Result Schema

Each test result includes:

```typescript
{
  promptId: string;
  promptDescription: string;
  mcpServerName: string;
  iteration: number;  // Which iteration of this test (1-indexed)
  success: boolean;
  error?: string;
  inputTokens: number;
  outputTokens: number;
  totalTokens: number;
  toolsUsedCount: number;
  toolsCalled: string[];
  durationMs: number;
  model: string;
}
```

### Analyzing Multiple Iterations

When `numIterations` is set to more than 1, each prompt will be run multiple times. The results array will contain all iterations with the `iteration` field identifying each run. You can use this data to:

- Calculate average token usage across iterations
- Identify variance in tool calls
- Measure performance consistency
- Detect outliers in execution time

Example analysis (outside this tool):
```javascript
// Group by promptId and calculate averages
const byPrompt = results.reduce((acc, r) => {
  if (!acc[r.promptId]) acc[r.promptId] = [];
  acc[r.promptId].push(r);
  return acc;
}, {});

Object.entries(byPrompt).forEach(([promptId, runs]) => {
  const avgTokens = runs.reduce((sum, r) => sum + r.totalTokens, 0) / runs.length;
  console.log(`${promptId}: avg ${avgTokens.toFixed(2)} tokens`);
});
```

## Testing Custom MCP Servers

To test your own MCP server:

1. Add it to the `mcpServers` array in your config:

```json
{
  "name": "my-custom-server",
  "command": "node",
  "args": ["path/to/your/server.js"],
  "env": {
    "API_KEY": "your-api-key"
  }
}
```

2. Create prompts that exercise your server's tools:

```json
{
  "id": "custom-test",
  "description": "Test my custom tool",
  "prompt": "Use the custom tool to fetch data about X"
}
```

## Development

Run in watch mode for development:

```bash
pnpm dev
```

Type check:

```bash
pnpm type-check
```

## Architecture

- **types.ts**: TypeScript type definitions
- **mcp-client.ts**: MCP server connection and management
- **test-runner.ts**: Main test execution and metrics collection
- **run-tests.ts**: CLI entry point and configuration loading

## Tips

- Start with a small number of prompts to verify your MCP server is working
- Use `expectedTools` in prompts to document which tools should be called
- Compare token usage across different models or MCP server implementations
- Run tests multiple times to identify performance variations
- Use the JSON output for further analysis or visualization
