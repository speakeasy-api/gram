# Test Plan: MCP Schema Compatibility Fix (AGE-904)

## Overview
This test plan verifies that the fix for stripping `$schema` fields from MCP tool schemas resolves Gemini CLI compatibility issues.

## Prerequisites
- Gemini CLI installed: `npm install -g @google/gemini-cli`
- Gram CLI installed and configured
- Access to a deployed Gram project with a test toolset

## Test Case 1: Verify Schema Field Removal (Unit Test)

**Status:** ✅ PASSING

```bash
cd server
go test ./internal/conv -v -run TestStripSchemaField
```

**Expected Result:**
- All tests pass
- Confirms `$schema` field is removed
- Confirms other fields are preserved

## Test Case 2: End-to-End Gemini CLI Integration

### Setup
1. Deploy a test Gram function (hello-world example):
   ```bash
   pnpm create @gram-ai/function@latest --template gram
   cd hello-world
   pnpm build
   pnpm push
   ```

2. Install the toolset in Gemini CLI:
   ```bash
   gram install gemini-cli --toolset hello-world
   ```

3. Verify the installation:
   ```bash
   gemini mcp list
   ```

### Test Execution

#### Before Fix (Expected Failure)
```bash
gemini "say hello to test-user"
```

**Expected Error (on old server):**
```
x  greet (Hello World MCP Server) {"name":"test-user"}

   no schema with key or ref "https://json-schema.org/draft/2020-12/schema"
```

#### After Fix (Expected Success)
```bash
gemini "say hello to test-user"
```

**Expected Success:**
```
✓  greet (Hello World MCP Server) {"name":"test-user"}

   Result: Hello, test-user! Welcome to Gram Functions.
```

## Test Case 3: Verify Schema Structure

### Manual Schema Inspection

1. Start Gemini CLI in debug mode:
   ```bash
   gemini --debug "list available tools"
   ```

2. Inspect the MCP `tools/list` response in debug output

3. Verify that tool schemas:
   - ✅ Do NOT contain `$schema` field
   - ✅ DO contain `type`, `properties`, `required` fields
   - ✅ Maintain all original schema properties

### Using curl to inspect directly

```bash
# Replace with your MCP server URL
MCP_URL="https://mcp.getgram.ai/mcp/YOUR_SLUG"
API_KEY="your-api-key"

curl -X POST "$MCP_URL" \
  -H "Content-Type: application/json" \
  -H "Authorization: $API_KEY" \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "tools/list"
  }' | jq '.result.tools[0].inputSchema'
```

**Expected Output (After Fix):**
```json
{
  "type": "object",
  "properties": {
    "name": {
      "type": "string",
      "description": "Name of the person to greet"
    }
  },
  "required": ["name"]
}
```

**Note:** Should NOT contain `"$schema": "https://json-schema.org/draft/2020-12/schema"`

## Test Case 4: Environment Variable Authentication

**Status:** ⚠️ KNOWN LIMITATION (Gemini CLI issue, not Gram bug)

When installing a toolset that requires environment variables:

```bash
gram install gemini-cli --toolset taskmaster
```

**Expected Behavior:**
1. Installation succeeds with warning about setting environment variable
2. Before setting env var: Gemini CLI may show OAuth error (this is a Gemini CLI UX issue)
3. After setting env var: Tools should work correctly

**Workaround:**
Set the environment variable before starting Gemini CLI:
```bash
export TASKMASTER_API_KEY='your-key'
gemini "your prompt here"
```

## Test Case 5: Backward Compatibility

Verify that existing MCP clients (Claude Desktop, Claude Code, Cursor) continue to work:

1. Test with Claude Desktop (if available)
2. Test with Claude Code
3. Test with Cursor

All should work identically before and after the fix, as we're only removing an optional field.

## Rollback Plan

If issues are discovered after deployment:

1. Revert PR #898
2. Redeploy previous version
3. Investigate reported issues
4. Update fix and redeploy

## Success Criteria

- ✅ Unit tests pass
- ✅ Gemini CLI can execute tools without schema errors
- ✅ Tool schemas maintain all required fields
- ✅ No regressions with other MCP clients
- ✅ Performance impact is negligible (JSON parse/serialize is fast)

## Notes

- The fix only affects MCP responses, not database storage
- Original OpenAPI-generated schemas remain unchanged
- The `$schema` field is optional per JSON Schema spec, so removal is safe
- Fix is applied at the response boundary in `conv.ToToolListEntry()`
