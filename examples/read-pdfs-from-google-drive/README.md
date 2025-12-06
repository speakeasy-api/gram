# Gram Functions: PDF Reader with Google Drive

This example demonstrates how to build [Gram Functions](https://www.speakeasy.com/docs/gram/gram-functions/introduction) that interact with Google Drive using OAuth2 authentication to search for and read PDF files. Perfect for building document processing workflows with LLMs that need access to user-owned Google Drive content.

## What's Included

**`search_files` Tool**

Search for PDF files in Google Drive. Demonstrates:

- Google Drive API integration with OAuth2 authentication
- Filename-based search filtering
- Optional folder and file type filtering
- Structured result formatting with file metadata

**`read_pdf` Tool**

Extract text content from PDF files stored in Google Drive. Demonstrates:

- Authenticated file downloads from Google Drive
- PDF parsing and text extraction
- Metadata extraction (page count, document info)
- Content truncation for large documents

## Key Patterns

- **OAuth2 authentication** using the `oauthVariable` configuration
- **Google Drive integration** with the official `@googleapis/drive` SDK
- **PDF processing** with `pdf2json` for text extraction
- **Type-safe validation** with Zod schemas
- **Error handling** with `ctx.fail()`
- **Modular architecture** separating Drive operations from PDF processing

## Setup

### Prerequisites

1. A Google Cloud project with Drive API enabled
2. OAuth2 credentials configured for your Gram deployment
3. User authorization to access Google Drive files

### OAuth Configuration

This example uses **OAuth2 authentication** to access user-owned Google Drive files. The key configuration is the `oauthVariable` setting in [gram.ts:11](src/gram.ts#L11):

```typescript
const gram = new Gram({
  envSchema: {
    GOOGLE_ACCESS_TOKEN: z.string().describe("Google OAuth2 access token"),
  },
  authInput: {
    oauthVariable: "GOOGLE_ACCESS_TOKEN",
  },
})
```

The `oauthVariable` tells Gram that this function requires OAuth2 authentication and specifies which environment variable will receive the access token. When users invoke this function, Gram automatically handles the OAuth2 flow and injects the access token into the `GOOGLE_ACCESS_TOKEN` environment variable.

**Learn more about configuring OAuth for Gram Functions:**
📖 [Gram OAuth Configuration Documentation](https://www.speakeasy.com/docs/gram/gram-functions/oauth)

### Google Cloud Setup

1. **Enable the Google Drive API** in your Google Cloud project
2. **Create OAuth2 credentials**:
   - Go to Google Cloud Console → APIs & Services → Credentials
   - Create OAuth 2.0 Client ID (Web application type)
   - Configure authorized redirect URIs for your Gram deployment
3. **Configure OAuth scopes**:
   - `https://www.googleapis.com/auth/drive.readonly` - Read-only access to Drive files
   - `https://www.googleapis.com/auth/drive.metadata.readonly` - Read file metadata

## Quick Start

Install dependencies:

```bash
npm install
```

Build a deployment package:

```bash
npm build
```

Push your function to Gram:

```bash
npm push
```

## Testing Locally

Test your tools during development using the MCP Inspector:

```bash
npm dev
```

This starts a local MCP server over stdio transport, allowing you to interactively test both tools.

**Note:** For local testing, you'll need to manually provide a valid Google OAuth2 access token. You can obtain one using the [OAuth 2.0 Playground](https://developers.google.com/oauthplayground/) or by implementing a local OAuth flow.

### Example Usage

**Search for files:**
```json
{
  "query": "quarterly report",
  "folderId": "1BxiMVs0XRA5nFMdKvBdBZjgmUUqptlbs74OgvE2upms",
  "fileType": "application/pdf"
}
```

**Read a PDF:**
```json
{
  "fileId": "1BxiMVs0XRA5nFMdKvBdBZjgmUUqptlbs74OgvE2upms"
}
```

## Project Structure

```
read-pdfs-from-google-drive/
├── src/
│   ├── gram.ts       # Tool definitions (search_files, read_pdf)
│   ├── drive.ts      # Google Drive API client operations
│   ├── pdf.ts        # PDF parsing logic
│   ├── oauth.ts      # OAuth2 helper utilities
│   └── server.ts     # MCP server setup
├── package.json
├── tsconfig.json
└── gram.config.ts
```

## Learn More

- [Gram Functions Documentation](https://www.speakeasy.com/docs/gram/gram-functions/introduction)
- [Gram OAuth Configuration](https://www.speakeasy.com/docs/gram/gram-functions/oauth)
- [Google Drive API Documentation](https://developers.google.com/drive/api/v3/about-sdk)
- [Google OAuth 2.0 Documentation](https://developers.google.com/identity/protocols/oauth2)
