import { Gram } from "@gram-ai/functions";
import { z } from "zod";
import * as drive from "./drive.ts";
import * as pdf from "./pdf.ts";

const gram = new Gram({
  envSchema: {
    GOOGLE_ACCESS_TOKEN: z.string().describe("Google OAuth2 access token"),
  },
  authInput: {
    oauthVariable: "GOOGLE_ACCESS_TOKEN",
  },
})
  .tool({
    name: "search_files",
    description:
      "Search for PDF files in Google Drive. Takes a search query and returns matching files based on their filename.",
    inputSchema: {
      query: z
        .string()
        .describe(
          "Search query to match against filenames (e.g., 'report' will match files containing 'report' in the name)"
        ),
      folderId: z
        .string()
        .optional()
        .describe(
          "Optional: Specific Google Drive folder ID to search within. If not provided, searches entire accessible drive."
        ),
      fileType: z
        .string()
        .optional()
        .describe(
          "Optional: File type to search for (e.g., 'application/pdf')"
        ),
    },
    async execute(ctx, input) {
      const { GOOGLE_ACCESS_TOKEN } = ctx.env;

      if (!GOOGLE_ACCESS_TOKEN) {
        return ctx.fail({
          error:
            "Google OAuth credentials not configured. Please set GOOGLE_ACCESS_TOKEN environment variable.",
        });
      }

      try {
        // Search for PDF files in Google Drive
        const matchingFiles = await drive.searchFiles(
          GOOGLE_ACCESS_TOKEN,
          input.query,
          input.folderId,
          input.fileType
        );

        return ctx.json({
          query: input.query,
          folderId: input.folderId || "root",
          totalMatches: matchingFiles.length,
          files: matchingFiles.map((file) => ({
            id: file.id,
            name: file.name,
            size: file.size,
            modifiedTime: file.modifiedTime,
            webViewLink: file.webViewLink,
          })),
        });
      } catch (error) {
        return ctx.fail({
          error: `Failed to search Google Drive: ${
            error instanceof Error ? error.message : "Unknown error"
          }`,
        });
      }
    },
  })
  .tool({
    name: "read_pdf",
    description:
      "Extract text content from a PDF file stored in Google Drive. Provide the Google Drive file ID.",
    inputSchema: {
      fileId: z
        .string()
        .describe(
          "Google Drive file ID of the PDF file (e.g., '1BxiMVs0XRA5nFMdKvBdBZjgmUUqptlbs74OgvE2upms')"
        ),
    },
    async execute(ctx, input) {
      const { GOOGLE_ACCESS_TOKEN } = ctx.env;

      if (!GOOGLE_ACCESS_TOKEN) {
        return ctx.fail({
          error:
            "Google OAuth credentials not configured. Please set GOOGLE_ACCESS_TOKEN environment variable.",
        });
      }

      try {
        // Extract text from the PDF
        const content = await pdf.extractTextFromPDF(
          GOOGLE_ACCESS_TOKEN,
          input.fileId
        );

        // Limit response size to prevent EOF errors (max ~100KB of text)
        const MAX_TEXT_LENGTH = 100000;
        const truncated = content.text.length > MAX_TEXT_LENGTH;
        const text = truncated
          ? content.text.slice(0, MAX_TEXT_LENGTH) + "\n\n[... truncated ...]"
          : content.text;

        return ctx.json({
          fileId: input.fileId,
          text,
          numPages: content.numPages,
          truncated,
          totalCharacters: content.text.length,
          metadata: content.info,
        });
      } catch (error) {
        return ctx.fail({
          error: `Failed to read PDF from Google Drive: ${
            error instanceof Error ? error.message : "Unknown error"
          }`,
        });
      }
    },
  });

export default gram;
