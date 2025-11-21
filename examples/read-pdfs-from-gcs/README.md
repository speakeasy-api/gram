# Gram Functions: PDF Reader with Google Cloud Storage

This example demonstrates how to build [Gram Functions](https://www.speakeasy.com/docs/gram/gram-functions/introduction) that interact with Google Cloud Storage to search for and read PDF files. Perfect for building document processing workflows with LLMs.

## What's Included

**`search_files` Tool**

Search for PDF files in a GCS bucket. Demonstrates:

- GCS bucket integration and file listing
- Simple filename-based search filtering
- Environment variable configuration
- Structured result formatting

**`read_pdf` Tool**

Extract text content from PDF files. Demonstrates:

- Downloading files from GCS
- PDF parsing and text extraction
- Metadata extraction (page count, document info)
- Error handling for invalid paths

## Key Patterns

- **Google Cloud Storage integration** with the official `@google-cloud/storage` SDK
- **PDF processing** with `pdf-parse` for text extraction
- **Type-safe validation** with Zod schemas
- **Error handling** with `ctx.fail()`
- **Modular architecture** separating GCS operations from PDF processing

## Setup

### Prerequisites

1. A Google Cloud project with Cloud Storage enabled
2. A GCS bucket with PDF files
3. Google Cloud credentials configured (see [Authentication](#authentication) below)

### Authentication

The Google Cloud Storage client uses Application Default Credentials (ADC). Set up authentication using one of these methods:

**Option 1: Service Account Key (Development)**

1. Create a service account in your GCP project
2. Download the JSON key file
3. Set the `GOOGLE_APPLICATION_CREDENTIALS` environment variable:

```bash
export GOOGLE_APPLICATION_CREDENTIALS="/path/to/your-service-account-key.json"
```

**Option 2: gcloud CLI (Development)**

```bash
gcloud auth application-default login
```

**Option 3: Workload Identity (Production)**

For production deployments, use [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity) to automatically authenticate without managing keys.

### Environment Variables

Create a `.env` file based on `.env.example`:

```bash
# Required: GCS bucket path to search for PDFs
GCS_BUCKET_PATH=gs://your-bucket-name/optional/prefix

# Optional: Path to service account key (if not using gcloud auth)
# GOOGLE_APPLICATION_CREDENTIALS=/path/to/key.json
```

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

### Example Usage

**Search for files:**
```json
{
  "query": "report"
}
```

**Read a PDF:**
```json
{
  "filePath": "gs://your-bucket/documents/report.pdf"
}
```

## Project Structure

```
pdf-reader/
├── src/
│   ├── gram.ts       # Tool definitions (search_files, read_pdf)
│   ├── gcs.ts        # GCS client operations
│   ├── pdf.ts        # PDF parsing logic
│   └── server.ts     # MCP server setup
├── package.json
├── tsconfig.json
└── gram.config.ts
```

## Learn More

- [Gram Functions Documentation](https://www.speakeasy.com/docs/gram/gram-functions/introduction)
- [Google Cloud Storage Node.js Client](https://cloud.google.com/nodejs/docs/reference/storage/latest)
- [pdf-parse Library](https://www.npmjs.com/package/pdf-parse)
