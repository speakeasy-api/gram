import { Gram } from "@gram-ai/functions";
import { z } from "zod";
import * as gcs from "./gcs.ts";
import * as pdf from "./pdf.ts";

const gram = new Gram({
  envSchema: {
    GCS_BUCKET_NAME: z
      .string()
      .describe(
        "The name of the Google Cloud Storage bucket to search for PDFs. Must be public."
      ),
  },
})
  .tool({
    name: "search_files",
    description:
      "Simple search for PDF files in a Google Cloud Storage bucket. Takes a search query and returns matching files based on their filename.",
    inputSchema: {
      query: z
        .string()
        .describe(
          "Search query to match against filenames (e.g., 'dog' will match 'my_dogs.pdf')"
        ),
    },
    async execute(ctx, input) {
      const bucketName = ctx.env.GCS_BUCKET_NAME;
      if (!bucketName) {
        return ctx.fail({
          error: "GCS_BUCKET_NAME environment variable is not set",
        });
      }

      // List all files in the bucket
      const allFiles = await gcs.listFiles(bucketName);

      // Filter files based on the search query
      const matchingFiles = gcs.searchFiles(allFiles, input.query);

      return ctx.json({
        bucketName,
        query: input.query,
        totalFiles: allFiles.length,
        matchingFiles: matchingFiles.map((file) => ({
          name: file.name,
          fullPath: file.fullPath,
          size: file.size,
          updated: file.updated,
        })),
      });
    },
  })
  .tool({
    name: "read_pdf",
    description:
      "Extract text content from a PDF file stored in Google Cloud Storage. Provide the full GCS path (gs://bucket-name/path/to/file.pdf).",
    inputSchema: {
      filePath: z
        .string()
        .describe(
          "Full GCS path to the PDF file (e.g., 'gs://my-bucket/documents/report.pdf')"
        ),
    },
    async execute(ctx, input) {
      // Validate that the file path is a GCS path
      if (!input.filePath.startsWith("gs://")) {
        return ctx.fail({
          error: "File path must be a valid GCS path starting with 'gs://'",
        });
      }

      // Extract text from the PDF
      const content = await pdf.extractTextFromPDF(input.filePath);

      // Limit response size to prevent EOF errors (max ~100KB of text)
      const MAX_TEXT_LENGTH = 100000;
      const truncated = content.text.length > MAX_TEXT_LENGTH;
      const text = truncated
        ? content.text.slice(0, MAX_TEXT_LENGTH) + "\n\n[... truncated ...]"
        : content.text;

      return ctx.json({
        filePath: input.filePath,
        text,
        numPages: content.numPages,
        truncated,
        totalCharacters: content.text.length,
        metadata: content.info,
      });
    },
  });

export default gram;
