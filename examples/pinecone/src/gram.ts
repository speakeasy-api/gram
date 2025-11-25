import { Gram } from "@gram-ai/functions";
import { OpenRouter } from "@openrouter/sdk";
import { Pinecone } from "@pinecone-database/pinecone";
import { z } from "zod";

// To learn more about Gram Functions, check out our documentation at:
// https://www.speakeasy.com/docs/gram/gram-functions/functions-framework
const gram = new Gram({
  envSchema: {
    PINECONE_API_KEY: z.string().describe("Pinecone API key"),
    OPENROUTER_API_KEY: z
      .string()
      .describe("OpenRouter API key for generating embeddings"),
  },
}).tool({
  name: "search",
  description:
    "Search for movies using semantic similarity based on embeddings",
  inputSchema: {
    query: z.string().describe("The search query to find similar movies"),
    limit: z
      .number()
      .min(1)
      .max(100)
      .optional()
      .describe("Maximum number of results to return (default: 10)"),
  },
  async execute(ctx, input) {
    const limit = input.limit ?? 10;

    // Initialize OpenRouter client
    const openrouter = new OpenRouter({
      apiKey: ctx.env["OPENROUTER_API_KEY"],
    });

    // Generate embedding for the search query
    let queryEmbedding: number[];
    try {
      const embeddingResponse = await openrouter.embeddings.generate({
        model: "sentence-transformers/paraphrase-minilm-l6-v2",
        input: input.query,
        dimensions: 384,
      });

      // Check for SDK bug where response might be a string
      if (typeof embeddingResponse === "string") {
        return ctx.fail(
          { error: "Unexpected string response from embedding API" },
          { status: 500 },
        );
      }

      // Validate the embedding data exists and is in the correct format
      const embeddingData = embeddingResponse.data?.[0]?.embedding;
      if (!embeddingData || !Array.isArray(embeddingData)) {
        return ctx.fail(
          { error: "Failed to generate embedding for query" },
          { status: 500 },
        );
      }

      queryEmbedding = embeddingData;
    } catch (error) {
      return ctx.fail(
        {
          error: `Failed to generate embedding: ${error instanceof Error ? error.message : String(error)}`,
        },
        { status: 500 },
      );
    }

    // Connect to Pinecone
    const pc = new Pinecone({
      apiKey: ctx.env["PINECONE_API_KEY"],
    });

    try {
      // Get index reference (specify host for local development)
      const index = pc.index("movies", "http://localhost:5081");

      // Perform vector similarity search
      const queryResponse = await index.query({
        topK: limit,
        vector: queryEmbedding,
        includeMetadata: true,
      });

      return ctx.json({
        query: input.query,
        results: queryResponse.matches.map((match) => ({
          id: match.id,
          title: match.metadata?.["title"],
          overview: match.metadata?.["overview"],
          release_date: match.metadata?.["release_date"],
          genre: match.metadata?.["genre"],
          popularity: match.metadata?.["popularity"],
          vote_count: match.metadata?.["vote_count"],
          vote_average: match.metadata?.["vote_average"],
          original_language: match.metadata?.["original_language"],
          poster_url: match.metadata?.["poster_url"],
          similarity_score: match.score, // Pinecone returns similarity directly (0-1)
        })),
        count: queryResponse.matches.length,
      });
    } catch (error) {
      return ctx.fail(
        {
          error: `Pinecone query failed: ${error instanceof Error ? error.message : String(error)}`,
        },
        { status: 500 },
      );
    }
  },
});

export default gram;
