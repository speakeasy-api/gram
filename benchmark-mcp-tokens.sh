#!/bin/bash

# Script to benchmark token usage for different MCP server configurations
# Tests: big-40 only, ide only, no MCP servers

set -e

# Configuration
RESULTS_DIR="./mcp-benchmark-results"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
RUN_DIR="${RESULTS_DIR}/${TIMESTAMP}"
ITERATIONS=10

# MCP config file location
MCP_CONFIG="$HOME/.config/claude-code/config.toml"
MCP_CONFIG_BACKUP="${MCP_CONFIG}.backup"

# Create results directory
mkdir -p "${RUN_DIR}"

echo "Starting MCP Token Benchmark"
echo "Results will be saved to: ${RUN_DIR}"
echo "----------------------------------------"

# Backup original config
cp "${MCP_CONFIG}" "${MCP_CONFIG_BACKUP}"
echo "✓ Backed up MCP config to ${MCP_CONFIG_BACKUP}"

# Function to update MCP config for a specific test
update_mcp_config() {
    local config_type=$1

    case $config_type in
        "big40")
            cat > "${MCP_CONFIG}" << 'EOF'
[mcpServers.big-40]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-everything"]
EOF
            ;;
        "ide")
            cat > "${MCP_CONFIG}" << 'EOF'
[mcpServers.ide]
command = "npx"
args = ["-y", "@speakeasy/mcp-server-ide"]
EOF
            ;;
        "none")
            # Empty config - no MCP servers
            echo "# No MCP servers configured" > "${MCP_CONFIG}"
            ;;
    esac

    echo "✓ Updated MCP config for: ${config_type}"
    sleep 2  # Give time for config to be picked up
}

# Function to extract token count from context output
extract_tokens() {
    local output=$1
    # Extract the total tokens from "claude-sonnet-4-5-20250929 · 108k/200k tokens (54%)"
    echo "$output" | grep -o 'claude-sonnet-4-5-20250929 · [0-9.]*k/200k' | grep -o '[0-9.]*k' | head -1
}

# Function to run a single test iteration
run_test_iteration() {
    local config_name=$1
    local iteration=$2
    local output_file="${RUN_DIR}/${config_name}_iter${iteration}.log"

    echo "  Running iteration ${iteration}..."

    # Create a temporary file for the conversation
    local temp_chat=$(mktemp)

    # Get baseline tokens with /context
    echo "/context" > "${temp_chat}"

    # Send the query
    echo "list 3 hubspot deals" >> "${temp_chat}"

    # Get final token count
    echo "/context" >> "${temp_chat}"

    # Run claude with the conversation
    # Note: This assumes 'claude' CLI exists and can read from stdin
    {
        echo "=== Baseline Token Usage ==="
        echo "/context"
        echo ""
        echo "=== Query ==="
        echo "list 3 hubspot deals"
        echo ""
        echo "=== Results and Final Token Usage ==="
    } > "${output_file}"

    # Execute the conversation (this is a simplified version - actual implementation may vary)
    # In practice, you'd need to:
    # 1. Start a new claude session
    # 2. Run /context to get baseline
    # 3. Send the query
    # 4. Run /context again to get final usage
    # 5. Capture all output

    # For now, create a script that can be run manually or with expect
    cat > "${output_file}.script" << SCRIPT_EOF
#!/usr/bin/expect -f
set timeout 120

# Start claude
spawn claude

# Wait for prompt
expect "λ"

# Get baseline
send "/context\r"
expect "λ"

# Save baseline output
set baseline \$expect_out(buffer)

# Send query
send "list 3 hubspot deals\r"
expect "λ"

# Save query output
set query_result \$expect_out(buffer)

# Get final token usage
send "/context\r"
expect "λ"

# Save final output
set final_tokens \$expect_out(buffer)

# Exit
send "exit\r"
expect eof

# Write results to file
set fp [open "${output_file}" w]
puts \$fp "=== Baseline Token Usage ==="
puts \$fp \$baseline
puts \$fp ""
puts \$fp "=== Query Results ==="
puts \$fp \$query_result
puts \$fp ""
puts \$fp "=== Final Token Usage ==="
puts \$fp \$final_tokens
close \$fp
SCRIPT_EOF

    chmod +x "${output_file}.script"

    # Run the expect script
    if command -v expect &> /dev/null; then
        "${output_file}.script" 2>&1 || echo "Warning: Script execution had issues"
    else
        echo "Warning: 'expect' not found. Script created at ${output_file}.script for manual execution"
    fi

    rm -f "${temp_chat}"
}

# Test configurations
configs=("big40" "ide" "none")

# Run tests for each configuration
for config in "${configs[@]}"; do
    echo ""
    echo "========================================"
    echo "Testing configuration: ${config}"
    echo "========================================"

    # Update MCP config
    update_mcp_config "${config}"

    # Run iterations
    for i in $(seq 1 ${ITERATIONS}); do
        run_test_iteration "${config}" "${i}"
    done

    echo "✓ Completed ${ITERATIONS} iterations for ${config}"
done

# Restore original config
cp "${MCP_CONFIG_BACKUP}" "${MCP_CONFIG}"
rm "${MCP_CONFIG_BACKUP}"
echo ""
echo "✓ Restored original MCP config"

# Generate summary report
echo ""
echo "========================================"
echo "Generating Summary Report"
echo "========================================"

SUMMARY_FILE="${RUN_DIR}/summary.txt"

{
    echo "MCP Token Benchmark Summary"
    echo "Generated: $(date)"
    echo "Iterations per config: ${ITERATIONS}"
    echo ""

    for config in "${configs[@]}"; do
        echo "Configuration: ${config}"
        echo "----------------------------------------"

        # List all log files for this config
        for log in "${RUN_DIR}/${config}_iter"*.log; do
            if [ -f "$log" ]; then
                echo "  $(basename "$log")"
                # Try to extract token counts if the log was generated
                if grep -q "claude-sonnet" "$log" 2>/dev/null; then
                    tokens=$(extract_tokens "$(cat "$log")")
                    echo "    Tokens: ${tokens}"
                fi
            fi
        done
        echo ""
    done
} > "${SUMMARY_FILE}"

cat "${SUMMARY_FILE}"

echo ""
echo "✓ Benchmark complete!"
echo "Results saved to: ${RUN_DIR}"
echo "Summary: ${SUMMARY_FILE}"
