# MCP Token Benchmark Scripts

Automated scripts to measure token usage with different MCP server configurations.

## Quick Start

The easiest way to run the benchmark is with the Python script:

```bash
./benchmark_mcp_tokens.py
```

This will:
1. Test 3 configurations (big-40 only, ide only, no MCP servers)
2. Run 10 iterations for each configuration
3. Collect baseline and final token usage
4. Generate detailed logs and summary reports

## What It Tests

For each configuration, the script:
1. Gets baseline token usage (with `/context`)
2. Sends the query: "list 3 hubspot deals"
3. Gets final token usage (with `/context` again)
4. Records the difference (query overhead)

## Output

Results are saved to `./mcp-benchmark-results/TIMESTAMP/`:

- **Individual logs**: `{config}_iter{N}.log` - Full output for each iteration
- **summary.json**: Machine-readable results
- **summary.txt**: Human-readable summary with averages

## Configuration Details

### big40
Only the big-40 MCP server (modelcontextprotocol/server-everything) enabled.
This provides access to hubspot, dub, formance, and polar APIs.

### ide
Only the ide MCP server (@speakeasy/mcp-server-ide) enabled.
This provides IDE-related tools.

### none
No MCP servers enabled - baseline Claude Code.

## Alternative Scripts

### Semi-Automated (for manual testing)

If the Python script doesn't work with your Claude CLI setup:

```bash
./benchmark-mcp-simple.sh
```

This creates a test harness that you run manually:
1. Sets up directories for each config
2. Provides step-by-step instructions
3. You manually run Claude and copy/paste token outputs
4. Analysis scripts parse your saved outputs

### Full Bash Version (uses expect)

```bash
./benchmark-mcp-tokens.sh
```

Requires `expect` to be installed. Attempts to fully automate the Claude CLI interactions.

## Requirements

- Claude Code CLI (`claude` command)
- Python 3.8+ (for Python script)
- expect (optional, for bash automation script)

## Troubleshooting

### Claude CLI not found
Make sure Claude Code CLI is installed and in your PATH:
```bash
which claude
```

### Config changes not taking effect
The scripts wait 2 seconds after changing config. If Claude Code caches configs longer, you may need to manually restart between tests.

### Parsing errors
If token counts aren't being extracted, check the log files to see the actual output format and adjust the regex patterns in the scripts.

## Analyzing Results

The summary report shows:
- Average baseline tokens for each config
- Average final tokens after query
- Query overhead (difference)
- Breakdown by token category (MCP tools, system tools, messages, etc.)

Compare the "MCP tools" token counts across configs to see the cost of loading different MCP server tool definitions.

## Example Output

```
Configuration: big40
----------------------------------------
Successful runs: 10/10
Average baseline tokens: 59.3k
Average final tokens: 108.5k
Average query overhead: 49.2k

  Iteration 1:
    Baseline: 59.1k tokens
      - MCP tools: 43.3k
      - System tools: 13.7k
    Final: 108.2k tokens
      - MCP tools: 43.3k
      - Messages: 1.1k
```

## Notes

- Each iteration starts a fresh Claude Code session
- The original MCP config is always restored after completion
- Backup config is saved to `~/.config/claude-code/config.toml.backup`
