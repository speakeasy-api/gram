import { Gram } from "@gram-ai/functions";
import { OpenRouter } from "@openrouter/sdk";
import Turbopuffer from "@turbopuffer/turbopuffer";
import { z } from "zod";

// To learn more about Gram Functions, check out our documentation at:
// https://www.speakeasy.com/docs/gram/gram-functions/functions-framework
const gram = new Gram({
  envSchema: {
    TURBOPUFFER_API_KEY: z
      .string()
      .describe("Turbopuffer API key from turbopuffer.com/dashboard"),
    TURBOPUFFER_REGION: z.string().describe("Turbopuffer region"),
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

    // Initialize Turbopuffer client
    const tpuf = new Turbopuffer({
      region: ctx.env["TURBOPUFFER_REGION"],
      apiKey: ctx.env["TURBOPUFFER_API_KEY"],
    });
    const ns = tpuf.namespace("movies");

    try {
      // Perform vector similarity search
      const result = await ns.query({
        rank_by: ["vector", "ANN", queryEmbedding],
        top_k: limit,
        include_attributes: [
          "release_date",
          "title",
          "overview",
          "popularity",
          "vote_count",
          "vote_average",
          "original_language",
          "genre",
          "poster_url",
        ],
      });

      // Turbopuffer returns a response with a rows array
      return ctx.json({
        query: input.query,
        results:
          result.rows?.map((row: any) => ({
            id: row.id,
            title: row.attributes?.title,
            overview: row.attributes?.overview,
            release_date: row.attributes?.release_date,
            genre: row.attributes?.genre,
            popularity: row.attributes?.popularity,
            vote_count: row.attributes?.vote_count,
            vote_average: row.attributes?.vote_average,
            original_language: row.attributes?.original_language,
            poster_url: row.attributes?.poster_url,
            similarity_score: 1 - (row.dist || 0), // Convert distance to similarity score
          })) ?? [],
        count: result.rows?.length || 0,
      });
    } catch (error) {
      return ctx.fail(
        {
          error: `Turbopuffer query failed: ${error instanceof Error ? error.message : String(error)}`,
        },
        { status: 500 },
      );
    }
  },
});

export default gram;
