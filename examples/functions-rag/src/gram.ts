import { Gram } from "@gram-ai/functions";
import { openAsBlob } from "node:fs";
import { Ragie } from "ragie";
import * as z from "zod/mini";
import type { FetchResult, SearchResult } from "./types.ts";

const gram = new Gram({
  envSchema: {
    RAGIE_API_KEY: z.string(),
  },
})
  .tool({
    name: "list_partitions",
    description:
      "List all partitions in your Ragie account. Partitions are used to organize documents into logical groups.",
    inputSchema: {
      nameFilter: z.optional(z.string()),
    },
    async execute(ctx, input) {
      const ragie = new Ragie({ auth: ctx.env["RAGIE_API_KEY"] });
      const iterator = await ragie.partitions.list();

      // Collect all partitions from the iterator
      const allPartitions: Array<{
        id?: string;
        name?: string;
        metadata?: Record<string, unknown>;
      }> = [];

      for await (const response of iterator) {
        // Each response has a .result property with type PartitionList
        if (response.result?.partitions) {
          allPartitions.push(...response.result.partitions);
        }
      }

      let partitions = allPartitions;

      // Apply name filter if provided
      const nameFilter = input["nameFilter"];
      if (nameFilter) {
        const filter = nameFilter.toLowerCase();
        partitions = partitions.filter((p) =>
          p.name?.toLowerCase().includes(filter),
        );
      }

      return ctx.json({
        partitions: partitions.map((p) => ({
          id: p.id,
          name: p.name,
          metadata: p.metadata,
        })),
        count: partitions.length,
      });
    },
  })
  .tool({
    name: "upload_file",
    description:
      "Upload a document file to Ragie from a local file path. The file will be processed and indexed for RAG search.",
    inputSchema: {
      filePath: z.string(),
      partitionId: z.optional(z.string()),
      metadata: z.optional(z.record(z.string(), z.string())),
      mode: z.optional(z.string()),
    },
    async execute(ctx, input) {
      const ragie = new Ragie({ auth: ctx.env["RAGIE_API_KEY"] });

      const file = await openAsBlob(input["filePath"]);

      const result = await ragie.documents.create({
        file,
        partition: input["partitionId"],
        metadata: input["metadata"],
        mode: input["mode"] as any,
      });

      return ctx.json({
        id: result.id,
        status: result.status,
        name: result.name,
        partition: result.partition,
      });
    },
  })
  .tool({
    name: "upload_url",
    description:
      "Ingest a document from a URL. Ragie will download and process the document for RAG search.",
    inputSchema: {
      url: z.string(),
      partitionId: z.optional(z.string()),
      metadata: z.optional(z.record(z.string(), z.string())),
      mode: z.optional(z.string()),
    },
    async execute(ctx, input) {
      const ragie = new Ragie({ auth: ctx.env["RAGIE_API_KEY"] });

      const result = await ragie.documents.createDocumentFromUrl({
        url: input["url"],
        partition: input["partitionId"],
        metadata: input["metadata"],
        mode: input["mode"] as any,
      });

      return ctx.json({
        id: result.id,
        status: result.status,
        name: result.name,
        partition: result.partition,
      });
    },
  })
  .tool({
    name: "upload_text",
    description:
      "Create a document from raw text content. Useful for indexing text that isn't in a file.",
    inputSchema: {
      text: z.string(),
      name: z.string(),
      partitionId: z.optional(z.string()),
      metadata: z.optional(z.record(z.string(), z.string())),
    },
    async execute(ctx, input) {
      const ragie = new Ragie({ auth: ctx.env["RAGIE_API_KEY"] });

      const result = await ragie.documents.createRaw({
        name: input["name"],
        data: input["text"],
        partition: input["partitionId"],
        metadata: input["metadata"],
      });

      return ctx.json({
        id: result.id,
        status: result.status,
        name: result.name,
        partition: result.partition,
      });
    },
  })
  .tool({
    name: "search",
    description:
      "Perform a RAG (Retrieval-Augmented Generation) search across your documents. Returns relevant document chunks with similarity scores.",
    inputSchema: {
      query: z.string(),
      partitionIds: z.optional(z.array(z.string())),
      topK: z.optional(z.number()),
      rerank: z.optional(z.boolean()),
    },
    async execute(ctx, input) {
      const ragie = new Ragie({ auth: ctx.env["RAGIE_API_KEY"] });

      const result = await ragie.retrievals.retrieve({
        query: input["query"],
        partition: input["partitionIds"]?.[0],
        topK: input["topK"],
        rerank: input["rerank"],
      });

      const results: SearchResult[] = [];
      for (const chunk of result.scoredChunks || []) {
        results.push({
          id: chunk.id,
          url: chunk.metadata?.["source_url"] || "",
          title: chunk.documentName || "Untitled Document",
        });
      }

      return ctx.json({ results });
    },
  })
  .tool({
    name: "fetch",
    description:
      "Download a document by its ID and save it to disk. Returns the file path where the document was saved along with metadata.",
    inputSchema: {
      documentId: z.string(),
      partitionId: z.optional(z.string()),
      outputPath: z.optional(z.string()),
    },
    async execute(ctx, input) {
      const ragie = new Ragie({ auth: ctx.env["RAGIE_API_KEY"] });

      const result = await ragie.documents.getContent({
        documentId: input["documentId"],
        partition: input["partitionId"],
      });

      return ctx.json<FetchResult>({
        id: result.id,
        title: result.name,
        url:
          typeof result.metadata["source_url"] === "string"
            ? result.metadata["source_url"]
            : "",
        text: result.content,
        metadata: result.metadata,
      });
    },
  });

export default gram;
