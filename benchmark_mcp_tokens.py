#!/usr/bin/env python3
"""
Automated MCP Benchmark Script
Tests different MCP server configurations (big-40, big-100, big-200)
by enabling/disabling them in ~/.claude.json
"""

import subprocess
import os
import json
from pathlib import Path
from datetime import datetime
import time
import sys

# Configuration
RESULTS_DIR = Path("./mcp-benchmark-results")
ITERATIONS = 10
CLAUDE_JSON_PATH = Path.home() / ".claude.json"
PROJECT_DIR = Path.cwd()
TEST_QUERY = "List 3 hubspot deals"
DEBUG = False  # Set to True to see detailed execution logs

# MCP server names to test (one at a time)
MCP_SERVERS = ["big-40", "big-100", "big-200", "big-400"]


def run_claude_query(query: str, timeout: int = 120) -> str:
    """
    Run Claude CLI with a query and return the output
    Uses --verbose flag to get full conversation including tool calls
    """
    try:
        result = subprocess.run(
            ["claude", "--verbose", "-p"],
            input=query,
            capture_output=True,
            text=True,
            timeout=timeout,
            env={**os.environ, "CLAUDE_NO_COLOR": "1"}
        )
        return result.stdout + "\n\n=== STDERR ===\n" + result.stderr
    except subprocess.TimeoutExpired:
        return "ERROR: Timeout expired"
    except Exception as e:
        return f"ERROR: {e}"


def update_mcp_config(server_name: str):
    """Enable one MCP server and disable the others in ~/.claude.json"""
    if server_name not in MCP_SERVERS:
        raise ValueError(f"Unknown server: {server_name}")

    # Read current config
    with open(CLAUDE_JSON_PATH, 'r') as f:
        config = json.load(f)

    # Backup original if not already backed up
    backup_path = CLAUDE_JSON_PATH.with_suffix(".json.backup")
    if not backup_path.exists():
        with open(backup_path, 'w') as f:
            json.dump(config, f, indent=2)
        print(f"✓ Backed up original config to {backup_path}")

    # Get project config
    project_key = str(PROJECT_DIR)
    if project_key not in config.get("projects", {}):
        print(f"⚠ Warning: Project {project_key} not found in ~/.claude.json")
        return

    project_config = config["projects"][project_key]

    # Enable only the target server, disable the others
    disabled_servers = []
    for server in MCP_SERVERS:
        if server != server_name:
            disabled_servers.append(server)

    project_config["disabledMcpServers"] = disabled_servers

    # Write updated config
    with open(CLAUDE_JSON_PATH, 'w') as f:
        json.dump(config, f, indent=2)

    print(f"✓ Enabled {server_name}, disabled {disabled_servers}")


def restore_mcp_config():
    """Restore the original MCP configuration"""
    backup_path = CLAUDE_JSON_PATH.with_suffix(".json.backup")
    if backup_path.exists():
        with open(backup_path, 'r') as f:
            original_config = json.load(f)
        with open(CLAUDE_JSON_PATH, 'w') as f:
            json.dump(original_config, f, indent=2)
        backup_path.unlink()
        print("✓ Restored original MCP config")


def run_single_test(server_name: str, iteration: int, output_dir: Path) -> dict:
    """Run a single test iteration"""
    print(f"  Running iteration {iteration}...")

    timeout = 90 if iteration == 1 else 60

    # Run the query
    output = run_claude_query(TEST_QUERY, timeout=timeout)

    # Check if the query was successful
    success = "hubspot" in output.lower() and "deal" in output.lower()
    has_error = "error" in output.lower() or "approval" in output.lower()

    # Save output
    log_file = output_dir / f"{server_name}_iter{iteration}.log"
    with open(log_file, 'w') as f:
        f.write("=" * 80 + "\n")
        f.write(f"MCP Server: {server_name}\n")
        f.write(f"Iteration: {iteration}\n")
        f.write(f"Timestamp: {datetime.now().isoformat()}\n")
        f.write("=" * 80 + "\n\n")
        f.write(output)

    result = {
        "iteration": iteration,
        "success": success,
        "has_error": has_error,
        "output_length": len(output),
        "log_file": str(log_file)
    }

    status = "✓" if success else "✗"
    print(f"  {status} Iteration {iteration} complete")
    return result


def run_config_tests(server_name: str, output_dir: Path) -> list[dict]:
    """Run all iterations for a given MCP server configuration"""
    print(f"\n{'=' * 60}")
    print(f"Testing MCP server: {server_name}")
    print(f"{'=' * 60}")

    # Update MCP config to enable this server
    update_mcp_config(server_name)

    # Run iterations
    results = []
    for i in range(1, ITERATIONS + 1):
        try:
            result = run_single_test(server_name, i, output_dir)
            results.append(result)
        except Exception as e:
            print(f" ✗ Error: {e}")
            results.append({
                "iteration": i,
                "error": str(e)
            })

    print(f"✓ Completed {len(results)} iterations for {server_name}")
    return results


def generate_summary(all_results: dict, output_dir: Path):
    """Generate summary report"""
    print(f"\n{'=' * 60}")
    print("Generating Summary Report")
    print(f"{'=' * 60}\n")

    summary_file = output_dir / "summary.json"
    summary_txt = output_dir / "summary.txt"

    # Save JSON
    with open(summary_file, 'w') as f:
        json.dump(all_results, f, indent=2)

    # Generate text summary
    with open(summary_txt, 'w') as f:
        f.write("MCP Server Benchmark Summary\n")
        f.write(f"Generated: {datetime.now().isoformat()}\n")
        f.write(f"Iterations per server: {ITERATIONS}\n")
        f.write(f"Test query: {TEST_QUERY}\n")
        f.write("=" * 80 + "\n\n")

        for server_name, results in all_results.items():
            f.write(f"MCP Server: {server_name}\n")
            f.write("-" * 80 + "\n")

            # Count successful runs
            successful_runs = [r for r in results if r.get("success")]
            error_runs = [r for r in results if r.get("has_error")]

            f.write(f"Total runs: {len(results)}\n")
            f.write(f"Successful: {len(successful_runs)}\n")
            f.write(f"With errors: {len(error_runs)}\n")
            f.write("\n")

            # Detail each iteration
            for r in results:
                f.write(f"  Iteration {r['iteration']}:\n")
                f.write(f"    Success: {r.get('success', False)}\n")
                f.write(f"    Has error: {r.get('has_error', False)}\n")
                f.write(f"    Output length: {r.get('output_length', 0)} chars\n")
                f.write(f"    Log: {r.get('log_file', 'N/A')}\n")
                f.write("\n")

            f.write("\n")

    # Print summary to console
    with open(summary_txt, 'r') as f:
        print(f.read())

    print(f"✓ Summary saved to: {summary_file} and {summary_txt}")


def main():
    """Main execution function"""
    print("MCP Server Benchmark Tool")
    print(f"{'=' * 60}\n")

    # Create results directory
    timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
    output_dir = RESULTS_DIR / timestamp
    output_dir.mkdir(parents=True, exist_ok=True)

    print(f"Results directory: {output_dir}")
    print(f"Iterations per server: {ITERATIONS}")
    print(f"Test query: {TEST_QUERY}\n")

    try:
        # Run tests for each MCP server
        all_results = {}
        for server_name in MCP_SERVERS:
            results = run_config_tests(server_name, output_dir)
            all_results[server_name] = results

        # Generate summary
        generate_summary(all_results, output_dir)

        print(f"\n✓ Benchmark complete!")
        print(f"Results saved to: {output_dir}")

    except KeyboardInterrupt:
        print("\n\n✗ Benchmark interrupted by user")
        sys.exit(1)
    finally:
        # Always restore original config
        restore_mcp_config()


if __name__ == "__main__":
    main()
