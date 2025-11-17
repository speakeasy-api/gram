#!/bin/bash

# Simple MCP Token Benchmark Script
# This creates individual test scripts that can be run to collect token data

set -e

RESULTS_DIR="./mcp-benchmark-results"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
RUN_DIR="${RESULTS_DIR}/${TIMESTAMP}"
ITERATIONS=10

mkdir -p "${RUN_DIR}"

echo "MCP Token Benchmark Setup"
echo "Results directory: ${RUN_DIR}"
echo ""

# Create test scripts for each configuration
create_test_script() {
    local config_name=$1
    local config_content=$2

    local test_dir="${RUN_DIR}/${config_name}"
    mkdir -p "${test_dir}"

    # Create the MCP config file for this test
    echo "${config_content}" > "${test_dir}/config.toml"

    # Create the test runner script
    cat > "${test_dir}/run_tests.sh" << 'RUNNER_EOF'
#!/bin/bash

CONFIG_FILE="$(dirname "$0")/config.toml"
MCP_CONFIG="$HOME/.config/claude-code/config.toml"
BACKUP_CONFIG="${MCP_CONFIG}.backup.$$"
RESULTS_DIR="$(dirname "$0")/results"

mkdir -p "${RESULTS_DIR}"

# Backup current config
cp "${MCP_CONFIG}" "${BACKUP_CONFIG}"
echo "✓ Backed up current config"

# Install our test config
cp "${CONFIG_FILE}" "${MCP_CONFIG}"
echo "✓ Installed test config"

echo ""
echo "Please complete these steps manually:"
echo ""
echo "1. Restart Claude Code (if currently running)"
echo "2. For each iteration (1-10), run the following:"
echo "   - Start: claude"
echo "   - Enter: /context"
echo "   - Copy the output and save to: ${RESULTS_DIR}/iterN_baseline.txt"
echo "   - Enter: list 3 hubspot deals"
echo "   - After completion, enter: /context"
echo "   - Copy the output and save to: ${RESULTS_DIR}/iterN_final.txt"
echo "   - Exit Claude Code"
echo ""
echo "3. When done with all 10 iterations, press Enter to restore your config"
read -p "Press Enter when all tests are complete..."

# Restore original config
cp "${BACKUP_CONFIG}" "${MCP_CONFIG}"
rm "${BACKUP_CONFIG}"

echo "✓ Restored original config"
echo "✓ Test complete. Results in: ${RESULTS_DIR}"
RUNNER_EOF

    chmod +x "${test_dir}/run_tests.sh"

    # Create a helper script to parse results
    cat > "${test_dir}/analyze_results.sh" << 'ANALYZE_EOF'
#!/bin/bash

RESULTS_DIR="$(dirname "$0")/results"

echo "Token Usage Analysis"
echo "===================="
echo ""

for i in {1..10}; do
    baseline_file="${RESULTS_DIR}/iter${i}_baseline.txt"
    final_file="${RESULTS_DIR}/iter${i}_final.txt"

    if [ -f "${baseline_file}" ] && [ -f "${final_file}" ]; then
        baseline_tokens=$(grep -o 'claude-sonnet-4-5-20250929 · [0-9.]*k/200k' "${baseline_file}" | grep -o '[0-9.]*k' | head -1)
        final_tokens=$(grep -o 'claude-sonnet-4-5-20250929 · [0-9.]*k/200k' "${final_file}" | grep -o '[0-9.]*k' | head -1)

        echo "Iteration ${i}:"
        echo "  Baseline: ${baseline_tokens} tokens"
        echo "  Final: ${final_tokens} tokens"
        echo ""
    fi
done
ANALYZE_EOF

    chmod +x "${test_dir}/analyze_results.sh"

    echo "Created test for: ${config_name} in ${test_dir}"
}

# Configuration 1: big-40 only
create_test_script "big40" '[mcpServers.big-40]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-everything"]'

# Configuration 2: ide only
create_test_script "ide" '[mcpServers.ide]
command = "npx"
args = ["-y", "@speakeasy/mcp-server-ide"]'

# Configuration 3: no MCP servers
create_test_script "none" '# No MCP servers configured'

# Create master summary script
cat > "${RUN_DIR}/summarize_all.sh" << 'SUMMARY_EOF'
#!/bin/bash

echo "==================================="
echo "MCP Token Benchmark Summary"
echo "==================================="
echo ""

for config_dir in big40 ide none; do
    echo "Configuration: ${config_dir}"
    echo "-----------------------------------"

    if [ -d "${config_dir}/results" ]; then
        cd "${config_dir}"
        ./analyze_results.sh
        cd ..
    else
        echo "  No results found"
    fi

    echo ""
done
SUMMARY_EOF

chmod +x "${RUN_DIR}/summarize_all.sh"

# Create README
cat > "${RUN_DIR}/README.md" << 'README_EOF'
# MCP Token Benchmark Test Suite

This directory contains automated test scripts to benchmark token usage across different MCP server configurations.

## Test Configurations

1. **big40**: Only the big-40 MCP server enabled
2. **ide**: Only the ide MCP server enabled
3. **none**: No MCP servers enabled

## Running the Tests

For each configuration directory (big40, ide, none):

1. Navigate to the directory:
   ```bash
   cd big40  # or ide, or none
   ```

2. Run the test script:
   ```bash
   ./run_tests.sh
   ```

3. Follow the on-screen instructions to manually execute each test iteration

4. After completing all iterations, run the analysis:
   ```bash
   ./analyze_results.sh
   ```

## Viewing Results

After completing all tests, return to this directory and run:

```bash
./summarize_all.sh
```

This will display a summary of all token usage measurements across all configurations.

## Manual Test Process

For each iteration:

1. Start Claude Code: `claude`
2. Get baseline tokens: `/context`
3. Save output to: `results/iter1_baseline.txt` (increment number for each iteration)
4. Send query: `list 3 hubspot deals`
5. Get final tokens: `/context`
6. Save output to: `results/iter1_final.txt`
7. Exit Claude Code
8. Repeat for iterations 2-10

## Tips

- Make sure to restart Claude Code between iterations for clean state
- Use copy/paste to save the /context output exactly
- The analyze script will automatically parse token counts from the saved files
README_EOF

echo ""
echo "✓ Setup complete!"
echo ""
echo "Next steps:"
echo "1. cd ${RUN_DIR}"
echo "2. Read the README.md for instructions"
echo "3. Run tests for each configuration (big40, ide, none)"
echo "4. Run ./summarize_all.sh to see results"
