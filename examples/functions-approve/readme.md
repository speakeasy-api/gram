# Gram MCP Approval Flow - Quick Start Guide

This guide demonstrates how to implement approval flows for MCP (Model Context Protocol) server tool calls using Gram Functions. The approval system provides a security layer that intercepts protected tool calls and validates admin approval before execution.

## 🎯 What This Solves

When building MCP servers, you often need to protect certain tools from unauthorized execution. This approval flow pattern allows you to:

- ✅ Intercept tool calls that require admin approval
- ✅ Validate approval status before execution
- ✅ Block unauthorized calls with clear error messages
- ✅ Provide audit trails and security controls
- ✅ Implement enterprise-grade access control

## 🚀 Quick Start

### 1. Understanding the Pattern

The approval flow uses a **proxy/interceptor pattern**:

```
User Request → Proxy Tool → Approval Check → Authorize/Block → Execute Tool
```

### 2. Basic Setup

```bash
# Set approval token (required for protected tools)
export MCP_TOOL_APPROVAL='approved'

# Restart your MCP server to load the environment variable
```

### 3. Using Protected Tools

```typescript
// Step 1: Check approval status
check_approval_status()

// Step 2: Execute protected tool via proxy
protected_tool_proxy(
  tool_name='protected_tool_create_resource',
  tool_arguments={name: 'example', type: 'database'}
)

// Step 3: If approved, execute the actual tool call
```

## 📋 Available Tools

### `protected_tool_proxy`

**Purpose**: Intercept and validate protected tool calls

**Usage**:
```typescript
protected_tool_proxy(
  tool_name: string,
  tool_arguments: Record<string, unknown>
)
```

**Returns**:
- `approved: true` → Tool call authorized, proceed with execution
- `approved: false` → Tool call blocked, approval required

**Example**:
```json
{
  "tool_name": "protected_tool_create_resource",
  "tool_arguments": {
    "name": "production_db",
    "type": "database"
  }
}
```

### `check_approval_status`

**Purpose**: Check current approval configuration

**Usage**:
```typescript
check_approval_status()
```

**Returns**: Approval status, environment variable state, and configuration

**Example Response**:
```json
{
  "approved": true,
  "message": "Admin approval verified via MCP_TOOL_APPROVAL environment variable",
  "environment_variable": {
    "name": "MCP_TOOL_APPROVAL",
    "set": true,
    "value": "appr..."
  }
}
```

### `list_protected_tools`

**Purpose**: Discover which tools require approval

**Usage**:
```typescript
list_protected_tools(filter?: string)
```

**Returns**: List of protected tool names

**Example**:
```typescript
list_protected_tools(filter='create')
// Returns tools matching 'create' pattern
```

### `help_approval_system`

**Purpose**: Get comprehensive documentation

**Usage**:
```typescript
help_approval_system()
```

**Returns**: Complete documentation, examples, and troubleshooting guide

## 🔧 Configuration

### Environment Variables

| Variable | Required | Description | Example |
|----------|----------|-------------|---------|
| `MCP_TOOL_APPROVAL` | Yes | Approval token for protected tools | `export MCP_TOOL_APPROVAL='approved'` |

### Customization Points

Edit `gram_approve_demo.ts` to customize:

1. **Tool Protection Pattern** (`CONFIG.PROTECTED_TOOL_PREFIX`)
   ```typescript
   PROTECTED_TOOL_PREFIX: "protected_tool_"
   ```

2. **Approval Validation** (`hasAdminApproval()`)
   - Environment variable (default)
   - API-based validation
   - Database-backed validation
   - Multi-factor validation

3. **Tool Filtering** (`requiresApproval()`)
   - Prefix matching
   - Suffix matching
   - Regex patterns
   - Exact match lists

## 🏗️ Architecture

### Approval Flow Diagram

```
┌─────────────┐
│  AI Agent   │
└──────┬──────┘
       │
       │ 1. Call protected_tool_proxy()
       ▼
┌─────────────────────┐
│  Proxy Tool         │
│  - Validates tool   │
│  - Checks approval  │
└──────┬──────────────┘
       │
       ├─── Approved? ────┐
       │                   │
       ▼                   ▼
┌─────────────┐    ┌─────────────┐
│   Blocked   │    │  Authorized │
│   Return    │    │  Return     │
│   Error     │    │  Instructions│
└─────────────┘    └──────┬───────┘
                          │
                          │ 2. Execute actual tool
                          ▼
                   ┌─────────────┐
                   │  MCP Server │
                   │  Tool Call  │
                   └─────────────┘
```

### Security Model

1. **Environment Variable Validation**
   - Simple and secure for single-server deployments
   - Easy to integrate with CI/CD pipelines
   - Suitable for development and testing

2. **API-Based Validation** (Advanced)
   - Call external approval service
   - Supports complex approval workflows
   - Enables audit logging

3. **Database-Backed Validation** (Enterprise)
   - Check approval status in database
   - Supports time-based approvals
   - Enables approval history tracking

## 📖 Examples

### Example 1: Basic Usage

```typescript
// 1. Check approval status
const status = await check_approval_status();
if (!status.approved) {
  console.error("Approval required!");
  return;
}

// 2. Execute protected tool
const result = await protected_tool_proxy({
  tool_name: "protected_tool_create_resource",
  tool_arguments: {
    name: "my_resource",
    type: "database"
  }
});

// 3. If approved, execute actual tool
if (result.approved) {
  await executeActualTool(result.tool_name, result.tool_arguments);
}
```

### Example 2: Error Handling

```typescript
try {
  const result = await protected_tool_proxy({
    tool_name: "protected_tool_delete_resource",
    tool_arguments: { id: "123" }
  });
  
  if (result.blocked) {
    console.error("Tool call blocked:", result.error);
    console.log("Instructions:", result.instructions);
  }
} catch (error) {
  console.error("Proxy error:", error);
}
```

### Example 3: Discovering Protected Tools

```typescript
// List all protected tools
const tools = await list_protected_tools();

// Filter by pattern
const createTools = await list_protected_tools({ filter: "create" });

console.log(`Found ${tools.total_tools} protected tools`);
```

## 🔒 Security Best Practices

1. **Never commit approval tokens to version control**
   ```bash
   # Add to .gitignore
   echo "*.env" >> .gitignore
   ```

2. **Use secure secret management**
   - Development: Environment variables
   - Production: Secret management services (AWS Secrets Manager, HashiCorp Vault, etc.)

3. **Rotate approval tokens regularly**
   - Set expiration dates
   - Implement token rotation policies

4. **Implement audit logging**
   ```typescript
   // Log all approval checks
   console.log({
     timestamp: new Date().toISOString(),
     tool: toolName,
     approved: approvalStatus.approved,
     user: getCurrentUser()
   });
   ```

5. **Use different tokens for different environments**
   ```bash
   # Development
   export MCP_TOOL_APPROVAL='dev_approved'
   
   # Production
   export MCP_TOOL_APPROVAL='prod_approved'
   ```

## 🐛 Troubleshooting

### Approval Denied

**Problem**: `check_approval_status()` returns `approved: false`

**Solutions**:
1. Verify environment variable is set:
   ```bash
   echo $MCP_TOOL_APPROVAL
   ```

2. Ensure value is non-empty:
   ```bash
   export MCP_TOOL_APPROVAL='approved'
   ```

3. Restart MCP server after setting variable

4. Check for typos in variable name

### Tool Not Intercepted

**Problem**: Tool call bypasses approval check

**Solutions**:
1. Verify tool name matches protection pattern:
   ```typescript
   // Tool must start with "protected_tool_"
   protected_tool_create_resource ✅
   create_resource ❌
   ```

2. Check `requiresApproval()` function logic

3. Use `list_protected_tools()` to see protected tools

### Environment Variable Not Recognized

**Problem**: Variable set but not detected

**Solutions**:
1. Verify `envSchema` includes approval variable

2. Check variable name matches `CONFIG.APPROVAL_ENV_VAR`

3. Restart MCP server after configuration changes

## 🔄 Adapting to Your Use Case

### Step 1: Update Configuration

```typescript
const CONFIG = {
  APPROVAL_ENV_VAR: "YOUR_APPROVAL_VAR",
  PROTECTED_TOOL_PREFIX: "your_prefix_",
  MIN_APPROVAL_TOKEN_LENGTH: 8,
};
```

### Step 2: Customize Tool Filtering

```typescript
function requiresApproval(toolName: string): boolean {
  // Your custom logic here
  return toolName.startsWith("admin_") || 
         toolName.includes("_delete") ||
         adminToolsList.includes(toolName);
}
```

### Step 3: Implement Custom Approval Logic

```typescript
function hasAdminApproval(): boolean {
  // Example: API-based validation
  const response = await fetch("https://your-approval-service.com/check");
  return response.ok;
  
  // Example: Database validation
  // const approval = await db.query("SELECT * FROM approvals WHERE ...");
  // return approval.exists;
}
```

### Step 4: Update Tool Descriptions

Replace generic descriptions with your specific use case:

```typescript
description: "Proxy for YOUR_SERVICE admin tool calls..."
```

## 📚 Additional Resources

- **Gram Functions Documentation**: [@gram-ai/functions](https://github.com/gram-ai/functions)
- **MCP Protocol**: [Model Context Protocol](https://modelcontextprotocol.io)
- **Security Patterns**: See `gram_approve_demo.ts` for commented examples

## 🤝 Contributing

When adapting this pattern:

1. Keep the core approval flow intact
2. Document your customizations
3. Add tests for approval logic
4. Update this README with your changes

## 📝 License

This is an educational example. Adapt freely for your use case.

---

**Questions?** Check `help_approval_system()` tool for comprehensive documentation.

