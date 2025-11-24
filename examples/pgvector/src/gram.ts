import { Gram } from "@gram-ai/functions";
import { OpenRouter } from "@openrouter/sdk";
import { Client } from "pg";
import { z } from "zod";

// To learn more about Gram Functions, check out our documentation at:
// https://www.speakeasy.com/docs/gram/gram-functions/functions-framework
const gram = new Gram({
  envSchema: {
    DATABASE_URL: z.string().describe("PostgreSQL connection string"),
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

    // Connect to PostgreSQL
    const client = new Client({
      connectionString: ctx.env["DATABASE_URL"],
    });

    try {
      await client.connect();

      // Perform vector similarity search
      // Using cosine distance operator (<->) from pgvector
      const query = `
          SELECT
            id,
            release_date,
            title,
            overview,
            popularity,
            vote_count,
            vote_average,
            original_language,
            genre,
            poster_url,
            created_at,
            updated_at,
            (embedding <-> $1::vector) as distance
          FROM movies
          ORDER BY distance
          LIMIT $2
        `;

      const result = await client.query(query, [
        JSON.stringify(queryEmbedding),
        limit,
      ]);

      return ctx.json({
        query: input.query,
        results: result.rows.map((row) => ({
          id: row.id,
          title: row.title,
          overview: row.overview,
          release_date: row.release_date,
          genre: row.genre,
          popularity: row.popularity,
          vote_count: row.vote_count,
          vote_average: row.vote_average,
          original_language: row.original_language,
          poster_url: row.poster_url,
          created_at: row.created_at,
          updated_at: row.updated_at,
          similarity_score: 1 - row.distance, // Convert distance to similarity score
        })),
        count: result.rows.length,
      });
    } catch (error) {
      return ctx.fail(
        {
          error: `Database query failed: ${error instanceof Error ? error.message : String(error)}`,
        },
        { status: 500 },
      );
    } finally {
      await client.end();
    }
  },
});

export default gram;
