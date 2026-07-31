# Google Drive and Docs MCP setup

Gram's catalog includes Google's official Google Drive and Google Docs remote
MCP servers:

| Source       | Endpoint                                 | Purpose                                                                       |
| ------------ | ---------------------------------------- | ----------------------------------------------------------------------------- |
| Google Drive | `https://drivemcp.googleapis.com/mcp/v1` | Find, read, and create files, including converting HTML to native Google Docs |
| Google Docs  | `https://docsmcp.googleapis.com/mcp/v1`  | Read document structure and apply rich `documents.batchUpdate` edits          |

Install both sources when an assistant needs to create and richly edit
documents. Gram gives each MCP server a distinct slug and namespaces its tools,
so Drive and Docs tools remain unambiguous when attached to the same assistant.

## Google Cloud configuration

Use a Google Cloud project owned by your organization. Enable the product APIs
and their MCP services:

```bash
gcloud services enable \
  drive.googleapis.com \
  docs.googleapis.com \
  drivemcp.googleapis.com \
  docsmcp.googleapis.com \
  --project=<PROJECT_ID>
```

In Google Auth Platform, configure the OAuth consent screen and add the scopes
for each server you install.

Drive:

```text
https://www.googleapis.com/auth/drive.readonly
https://www.googleapis.com/auth/drive.file
```

Docs:

```text
https://www.googleapis.com/auth/drive.readonly
https://www.googleapis.com/auth/drive.file
https://www.googleapis.com/auth/documents.readonly
https://www.googleapis.com/auth/documents
```

If the app uses the External audience while it is in testing, add every person
who will authenticate as a test user.

Create an OAuth 2.0 client with application type **Web application**. The client
ID and secret are customer-managed: they remain owned by your organization and
must not be shared outside the Gram authentication settings. Add Gram's callback
as an authorized redirect URI:

```text
https://<YOUR_GRAM_HOST>/mcp/remote_login_callback
```

The catalog detail page displays the exact callback for the current Gram
environment.

## Connect in Gram

1. In **Catalog**, add **Google Drive** and **Google Docs** to the project.
2. Open each new MCP server and go to **Settings → Authentication**.
3. Select **Start With Discovered Configuration**. Gram discovers Google's
   authorization and token endpoints from the official MCP endpoint.
4. Choose **Manual** client configuration, then enter the customer-owned client
   ID and secret.
5. Confirm that the scope override matches the scopes listed above, save, and
   complete Google authorization.
6. Attach both MCP servers to the same assistant.

Google does not expose dynamic client registration for these servers, so Gram
cannot create the OAuth client automatically.

## Verify rich document creation

Ask the assistant:

```text
Create a Google Doc with example tables for a basic RBAC permissions structure
and dummy text organized into sections with headings.
```

The intended flow is:

1. Drive `create_file` creates a native Google Doc. It can accept `textContent`
   with `contentMimeType` (including HTML) and convert supported content.
2. Docs `update_doc` applies headings, text styles, and table requests using
   `documents.batchUpdate`.
3. Docs `read_doc` reads the finished document back. Verify that its structural
   response includes the expected headings, styled ranges, and table rows and
   cells.

## Migrate from the community Workspace connector

1. Add and authenticate the two official sources without removing the existing
   connector.
2. Attach Drive and Docs to the assistant and run the verification prompt.
3. Read the created document back with `read_doc` and confirm its structure.
4. Update any saved instructions that refer to community-specific tool names to
   use `create_file`, `update_doc`, and `read_doc`.
5. Remove the community connector after dependent workflows have been verified.

See [Google's Workspace MCP configuration guide](https://developers.google.com/workspace/guides/configure-mcp-servers)
for current Google Cloud and OAuth requirements.
