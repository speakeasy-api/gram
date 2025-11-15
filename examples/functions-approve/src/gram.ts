/**
 * =============================================================================
 * GRAM MCP APPROVAL FLOW DEMONSTRATION
 * =============================================================================
 * 
 * This is an educational example demonstrating how to implement approval flows
 * for MCP (Model Context Protocol) server tool calls using Gram Functions.
 * 
 * APPROVAL FLOW PATTERN:
 * 1. Intercept tool calls that require approval
 * 2. Validate approval status (via environment variable, API, database, etc.)
 * 3. Block unauthorized calls with clear error messages
 * 4. Authorize approved calls and provide execution instructions
 * 
 * ADAPTATION GUIDE:
 * - Replace "protected_tool_" prefix with your tool naming pattern
 * - Customize approval validation logic (env vars, API calls, etc.)
 * - Modify tool filtering logic to match your MCP server's tools
 * - Add additional approval mechanisms (multi-factor, time-based, etc.)
 * 
 * =============================================================================
 */

import { Gram } from "@gram-ai/functions";
import * as z from "zod/mini";

// =============================================================================
// CONFIGURATION
// =============================================================================

/**
 * Configuration constants for the approval system.
 * Modify these to match your use case.
 */
const CONFIG = {
  // Environment variable name that stores approval status
  APPROVAL_ENV_VAR: "MCP_TOOL_APPROVAL",
  
  // Prefix pattern for tools that require approval
  // Example: "protected_tool_" means all tools starting with this prefix
  PROTECTED_TOOL_PREFIX: "protected_tool_",
  
  // Optional: Minimum approval token length for security
  MIN_APPROVAL_TOKEN_LENGTH: 1,
} as const;

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

/**
 * Check if a tool name requires approval protection.
 * 
 * This function implements pattern matching to identify which tools
 * should be intercepted. Common patterns:
 * - Prefix matching: tool.startsWith("protected_")
 * - Suffix matching: tool.endsWith("_admin")
 * - Regex matching: /^admin_/.test(tool)
 * - List-based: allowedTools.includes(tool)
 * 
 * @param toolName - The name of the tool to check
 * @returns true if the tool requires approval, false otherwise
 */
function requiresApproval(toolName: string): boolean {
  // Pattern 1: Prefix-based matching (most common)
  if (toolName.startsWith(CONFIG.PROTECTED_TOOL_PREFIX)) {
    return true;
  }
  
  // Pattern 2: Example of suffix-based matching (commented out)
  // if (toolName.endsWith("_admin")) {
  //   return true;
  // }
  
  // Pattern 3: Example of exact match list (commented out)
  // const adminTools = ["delete_user", "modify_permissions", "reset_system"];
  // if (adminTools.includes(toolName)) {
  //   return true;
  // }
  
  return false;
}

/**
 * Validate admin approval by checking environment variable.
 * 
 * APPROVAL VALIDATION STRATEGIES:
 * 
 * 1. Environment Variable (current implementation)
 *    - Simple and secure for single-server deployments
 *    - Easy to set via CI/CD pipelines
 *    - Good for development and testing
 * 
 * 2. API-based Validation (example commented below)
 *    - Call external approval service
 *    - Supports complex approval workflows
 *    - Enables audit logging
 * 
 * 3. Database-backed Validation
 *    - Check approval status in database
 *    - Supports time-based approvals
 *    - Enables approval history tracking
 * 
 * 4. Multi-factor Validation
 *    - Combine multiple approval sources
 *    - Require multiple approvals for sensitive operations
 *    - Implement approval chains
 * 
 * @returns true if approval is granted, false otherwise
 */
function hasAdminApproval(): boolean {
  // Strategy 1: Environment Variable Validation
  const approval = process.env[CONFIG.APPROVAL_ENV_VAR];
  
  // Check if approval is set and non-empty
  if (!approval || approval.trim().length < CONFIG.MIN_APPROVAL_TOKEN_LENGTH) {
    return false;
  }
  
  // Optional: Validate approval token format
  // Example: Require specific format like "approved_<timestamp>"
  // const approvalPattern = /^approved_\d+$/;
  // if (!approvalPattern.test(approval)) {
  //   return false;
  // }
  
  return true;
  
  // Strategy 2: API-based Validation (example)
  // try {
  //   const response = await fetch("https://approval-service.example.com/check", {
  //     method: "POST",
  //     headers: { "Authorization": `Bearer ${process.env.APPROVAL_API_KEY}` },
  //     body: JSON.stringify({ tool: toolName }),
  //   });
  //   const data = await response.json();
  //   return data.approved === true;
  // } catch (error) {
  //   console.error("Approval API error:", error);
  //   return false; // Fail closed - deny on error
  // }
  
  // Strategy 3: Database Validation (example)
  // const db = getDatabaseConnection();
  // const approval = await db.query(
  //   "SELECT approved FROM approvals WHERE tool_name = $1 AND expires_at > NOW()",
  //   [toolName]
  // );
  // return approval.rows.length > 0 && approval.rows[0].approved === true;
}

/**
 * Get approval status with user-friendly messaging.
 * 
 * This function provides structured feedback about approval status,
 * which is essential for debugging and user experience.
 * 
 * @returns Object containing approval status and message
 */
function getApprovalStatus(): { approved: boolean; message: string } {
  const approved = hasAdminApproval();
  
  if (approved) {
    return {
      approved: true,
      message: `Admin approval verified via ${CONFIG.APPROVAL_ENV_VAR} environment variable`,
    };
  }
  
  return {
    approved: false,
    message: `Admin approval required. Set ${CONFIG.APPROVAL_ENV_VAR} environment variable to proceed.`,
  };
}

/**
 * Mask sensitive values for logging/display purposes.
 * 
 * Security best practice: Never expose full approval tokens or secrets
 * in logs or API responses.
 * 
 * @param value - The value to mask
 * @param visibleChars - Number of characters to show at the start
 * @returns Masked value string
 */
function maskSensitiveValue(value: string, visibleChars: number = 4): string {
  if (value.length <= visibleChars) {
    return "***"; // Fully mask short values
  }
  return `${value.substring(0, visibleChars)}...`;
}

// =============================================================================
// GRAM TOOLS - APPROVAL SYSTEM
// =============================================================================

/**
 * Create Gram instance with environment schema.
 * 
 * The envSchema defines which environment variables are expected.
 * When deployed, users will be prompted to provide these values.
 */
const gram = new Gram({
  envSchema: {
    [CONFIG.APPROVAL_ENV_VAR]: z.optional(z.string()),
  },
})
  /**
   * TOOL 1: Tool Call Proxy/Interceptor
   * 
   * This is the core tool that intercepts protected tool calls and validates approval.
   * 
   * WORKFLOW:
   * 1. Receive tool name and arguments
   * 2. Check if tool requires approval
   * 3. Validate approval status
   * 4. Return authorization result or block with error
   * 
   * INTEGRATION PATTERN:
   * When an AI agent wants to call a protected tool:
   * - First call this proxy tool with the tool name and arguments
   * - If approved, proceed with the actual tool call
   * - If denied, show error and request approval
   */
  .tool({
    name: "protected_tool_proxy",
    description:
      `Proxy/interceptor for protected MCP tool calls. Requires admin approval via ` +
      `${CONFIG.APPROVAL_ENV_VAR} environment variable. This tool validates approval ` +
      `before allowing execution of protected tools.`,
    inputSchema: {
      tool_name: z.string(),
      tool_arguments: z.record(z.string(), z.unknown()),
    },
    async execute(ctx, input) {
      try {
        const toolName = input["tool_name"];
        const toolArguments = input["tool_arguments"] || {};

        // STEP 1: Validate that this tool requires approval
        if (!requiresApproval(toolName)) {
          return ctx.json({
            success: false,
            error: `Tool '${toolName}' does not require approval. ` +
                   `Only tools matching pattern '${CONFIG.PROTECTED_TOOL_PREFIX}*' are intercepted.`,
            intercepted: false,
            tool_name: toolName,
          });
        }

        // STEP 2: Check admin approval
        const approvalStatus = getApprovalStatus();
        
        if (!approvalStatus.approved) {
          // APPROVAL DENIED: Return error with clear instructions
          return ctx.json({
            success: false,
            error: approvalStatus.message,
            intercepted: true,
            tool_name: toolName,
            blocked: true,
            approval_required: true,
            instructions: [
              `Set ${CONFIG.APPROVAL_ENV_VAR} environment variable to proceed`,
              `Example: export ${CONFIG.APPROVAL_ENV_VAR}='approved'`,
              "Once set, retry the tool call",
            ],
            // Include tool details for transparency
            tool_arguments: toolArguments,
          });
        }

        // STEP 3: APPROVAL GRANTED - Return authorization and instructions
        return ctx.json({
          success: true,
          intercepted: true,
          approved: true,
          tool_name: toolName,
          tool_arguments: toolArguments,
          message: "Admin approval verified. Tool call authorized.",
          authorization: {
            status: "granted",
            timestamp: new Date().toISOString(),
            approval_method: "environment_variable",
          },
          instructions: {
            step: "execute_protected_tool",
            description: `Execute protected MCP tool: ${toolName}`,
            tool_name: toolName,
            tool_arguments: toolArguments,
            note: "This tool call has been approved. Execute the protected MCP tool with the provided arguments.",
          },
          next_steps: [
            `Execute the protected MCP tool: ${toolName}`,
            `Pass arguments: ${JSON.stringify(toolArguments, null, 2)}`,
            "Review the tool response for success or errors",
            "Log the execution for audit purposes",
          ],
        });
      } catch (error: any) {
        // ERROR HANDLING: Fail gracefully with error details
        return ctx.json({
          success: false,
          error: error.message || String(error),
          intercepted: true,
          error_type: "execution_error",
        });
      }
    },
  })
  
  /**
   * TOOL 2: Approval Status Checker
   * 
   * This tool allows users to check their current approval status without
   * attempting a tool call. Useful for debugging and setup verification.
   */
  .tool({
    name: "check_approval_status",
    description:
      `Check the current admin approval status for protected MCP tool calls. ` +
      `Returns whether ${CONFIG.APPROVAL_ENV_VAR} is set and valid.`,
    inputSchema: {},
    async execute(ctx) {
      try {
        const approvalStatus = getApprovalStatus();
        const approvalValue = process.env[CONFIG.APPROVAL_ENV_VAR];

        return ctx.json({
          success: true,
          approved: approvalStatus.approved,
          message: approvalStatus.message,
          environment_variable: {
            name: CONFIG.APPROVAL_ENV_VAR,
            set: approvalValue !== undefined,
            // Security: Mask the actual value in responses
            value: approvalValue ? maskSensitiveValue(approvalValue) : undefined,
            length: approvalValue?.length || 0,
          },
          configuration: {
            protected_tool_prefix: CONFIG.PROTECTED_TOOL_PREFIX,
            min_token_length: CONFIG.MIN_APPROVAL_TOKEN_LENGTH,
          },
          instructions: approvalStatus.approved
            ? [
                "✓ Approval is active",
                "You can proceed with protected tool calls",
                "Use protected_tool_proxy() to execute protected tools",
              ]
            : [
                `Set ${CONFIG.APPROVAL_ENV_VAR} environment variable`,
                `Example: export ${CONFIG.APPROVAL_ENV_VAR}='approved'`,
                "Then retry your protected tool call",
                "Restart your MCP server after setting the variable",
              ],
        });
      } catch (error: any) {
        return ctx.json({
          success: false,
          error: error.message || String(error),
        });
      }
    },
  })
  
  /**
   * TOOL 3: List Protected Tools
   * 
   * This tool helps users discover which tools are protected and require approval.
   * In production, you might query the MCP server dynamically for available tools.
   */
  .tool({
    name: "list_protected_tools",
    description:
      `List available protected MCP tools that require approval. ` +
      `Returns a list of tool names matching the protection pattern.`,
    inputSchema: {
      filter: z.optional(z.string()),
    },
    async execute(ctx, input) {
      try {
        const filter = input["filter"]?.toLowerCase() || "";

        // EXAMPLE: List of protected tools
        // In production, you would query your MCP server for available tools
        // and filter them based on the requiresApproval() function
        const exampleProtectedTools = [
          "protected_tool_create_resource",
          "protected_tool_delete_resource",
          "protected_tool_modify_permissions",
          "protected_tool_reset_configuration",
          "protected_tool_export_data",
          "protected_tool_import_data",
          "protected_tool_manage_users",
        ];

        // Filter tools if filter is provided
        const filteredTools = filter
          ? exampleProtectedTools.filter((tool) =>
              tool.toLowerCase().includes(filter)
            )
          : exampleProtectedTools;

        return ctx.json({
          success: true,
          total_tools: filteredTools.length,
          tools: filteredTools,
          filter_applied: filter || null,
          protection_pattern: CONFIG.PROTECTED_TOOL_PREFIX + "*",
          note: "This is an example list. In production, query your MCP server " +
                "for the complete list of available tools and filter by protection pattern.",
          usage: {
            example: `protected_tool_proxy(tool_name='protected_tool_create_resource', tool_arguments={...})`,
            approval_required: true,
            approval_env_var: CONFIG.APPROVAL_ENV_VAR,
          },
        });
      } catch (error: any) {
        return ctx.json({
          success: false,
          error: error.message || String(error),
        });
      }
    },
  })
  
  /**
   * TOOL 4: Help and Documentation
   * 
   * This tool provides comprehensive documentation about the approval system.
   * Essential for onboarding and troubleshooting.
   */
  .tool({
    name: "help_approval_system",
    description:
      "Get comprehensive help and documentation about the approval system.",
    inputSchema: {},
    async execute(ctx) {
      return ctx.json({
        approval_system: {
          purpose:
            "Intercept and validate protected MCP tool calls with admin approval",
          architecture: {
            pattern: "Proxy/Interceptor Pattern",
            description:
              "Tool calls are intercepted by a proxy tool that validates approval " +
              "before allowing execution. This provides a security layer without " +
              "modifying the underlying MCP server.",
          },
          security_model: {
            environment_variable: CONFIG.APPROVAL_ENV_VAR,
            required: true,
            description:
              `Must be set to a non-empty value to authorize protected tool calls`,
            examples: [
              `export ${CONFIG.APPROVAL_ENV_VAR}='approved'`,
              `export ${CONFIG.APPROVAL_ENV_VAR}='true'`,
              `export ${CONFIG.APPROVAL_ENV_VAR}='admin_token_123'`,
            ],
            security_notes: [
              "Never commit approval tokens to version control",
              "Use secure secret management in production",
              "Rotate approval tokens regularly",
              "Use different tokens for different environments",
            ],
          },
          workflow: [
            "1. User attempts to call a protected MCP tool",
            "2. Tool call is intercepted by protected_tool_proxy",
            `3. System checks for ${CONFIG.APPROVAL_ENV_VAR} environment variable`,
            "4. If approved: Tool call is authorized and instructions are returned",
            "5. If not approved: Tool call is blocked with error message",
            "6. User executes the actual tool call (if approved)",
          ],
          protected_tools: {
            pattern: CONFIG.PROTECTED_TOOL_PREFIX + "*",
            description:
              `All tools starting with '${CONFIG.PROTECTED_TOOL_PREFIX}' are intercepted`,
            examples: [
              "protected_tool_create_resource",
              "protected_tool_delete_resource",
              "protected_tool_modify_permissions",
            ],
            customization:
              "Modify requiresApproval() function to change protection pattern",
          },
        },
        available_tools: {
          protected_tool_proxy: {
            purpose: "Proxy/interceptor for protected MCP tool calls",
            usage: `protected_tool_proxy(tool_name='protected_tool_...', tool_arguments={...})`,
            returns: "Approval status and execution instructions",
            when_to_use: "Before calling any protected tool",
          },
          check_approval_status: {
            purpose: "Check current admin approval status",
            usage: "check_approval_status()",
            returns: "Current approval status and environment variable state",
            when_to_use: "To verify setup or debug approval issues",
          },
          list_protected_tools: {
            purpose: "List available protected MCP tools",
            usage: "list_protected_tools(filter='create')",
            returns: "List of tool names matching the protection pattern",
            when_to_use: "To discover which tools require approval",
          },
          help_approval_system: {
            purpose: "Get comprehensive approval system documentation",
            usage: "help_approval_system()",
            returns: "Complete documentation and examples",
            when_to_use: "When learning the system or troubleshooting",
          },
        },
        best_practices: [
          "Always check approval status before attempting protected tool calls",
          `Set ${CONFIG.APPROVAL_ENV_VAR} in your environment before use`,
          "Use protected_tool_proxy for all protected tool calls",
          "Review tool arguments before execution",
          "Monitor approval status regularly",
          "Use secure secret management in production",
          "Implement audit logging for approved tool calls",
          "Set up alerts for failed approval attempts",
        ],
        adaptation_guide: {
          step_1: "Modify CONFIG constants to match your tool naming",
          step_2: "Update requiresApproval() function with your protection logic",
          step_3: "Customize hasAdminApproval() with your approval mechanism",
          step_4: "Update tool descriptions and messages for your use case",
          step_5: "Add additional approval mechanisms as needed",
        },
        troubleshooting: {
          "Approval denied": [
            `Verify ${CONFIG.APPROVAL_ENV_VAR} is set: echo $${CONFIG.APPROVAL_ENV_VAR}`,
            "Ensure the value is non-empty",
            "Check for typos in environment variable name",
            "Restart your MCP server after setting the variable",
            "Verify the approval token meets minimum length requirements",
          ],
          "Tool not intercepted": [
            `Verify tool name starts with '${CONFIG.PROTECTED_TOOL_PREFIX}'`,
            "Use list_protected_tools() to see protected tools",
            "Check tool name spelling",
            "Review requiresApproval() function logic",
          ],
          "Environment variable not recognized": [
            "Check that envSchema includes your approval variable",
            "Verify variable name matches CONFIG.APPROVAL_ENV_VAR",
            "Restart MCP server after configuration changes",
          ],
        },
        examples: {
          check_status: {
            description: "Check if approval is configured",
            call: "check_approval_status()",
          },
          list_tools: {
            description: "Find all protected tools",
            call: "list_protected_tools()",
          },
          execute_protected: {
            description: "Execute a protected tool (with approval)",
            step_1: `Set ${CONFIG.APPROVAL_ENV_VAR}='approved'`,
            step_2: "protected_tool_proxy(tool_name='protected_tool_create_resource', tool_arguments={name: 'test'})",
            step_3: "Execute the actual tool call if approved",
          },
        },
      });
    },
  });

export default gram;