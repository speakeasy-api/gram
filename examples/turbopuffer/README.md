# Semantic Movie Search with Turbopuffer

This example demonstrates how to build a **semantic search** system using [Gram
Functions](https://getgram.ai), [Turbopuffer](https://turbopuffer.com)'s cloud-based
vector database, and OpenRouter's embedding API. Users can search for movies using
natural language queries, and the system returns semantically similar results based
on vector embeddings.

## What This Example Demonstrates

- **Vector similarity search** using Turbopuffer's cloud vector database
- **Semantic search** that understands meaning, not just keywords (e.g., "space adventure" matches "Star Wars")
- **Real-time embedding generation** using OpenRouter's API
- **Gram Functions framework** for building LLM-compatible tools
- **Cloud-based setup** with no local infrastructure required

## Why Turbopuffer?

Unlike traditional vector databases that require local setup or complex infrastructure:

- **Cloud-native**: No infrastructure to manage
- **Fast**: Single-digit millisecond p90 latency on millions of vectors
- **Simple**: No indexing strategy decisions, automatic optimization
- **Cost-effective**: Pay only for what you use
- **Scalable**: Handles datasets from thousands to billions of vectors

> **Comparison with pgvector**: Unlike the [pgvector example](../pgvector) which requires Docker and PostgreSQL, Turbopuffer is a fully-managed cloud service. This means faster setup, no local database management, and automatic scaling.

## Prerequisites

Before running this example, you'll need:

- **Node.js v22** or later ([Download Node.js](https://nodejs.org/))
- **npm** (comes with Node.js)
- **Turbopuffer API key** ([Sign up at Turbopuffer](https://turbopuffer.com/dashboard))
- **OpenRouter API key** ([Sign up at OpenRouter](https://openrouter.ai/))

## Setup Instructions

### 1. Install Dependencies

```bash
npm install
```

### 2. Create Turbopuffer Account

1. Visit [https://turbopuffer.com/dashboard](https://turbopuffer.com/dashboard)
2. Sign up for an account (free tier available)
3. Copy your API key from the dashboard

### 3. Get OpenRouter API Key

1. Visit [https://openrouter.ai/](https://openrouter.ai/)
2. Sign up for an account
3. Navigate to your API keys and create one

### 4. Configure Environment Variables

Create a `.env` file based on the provided example:

```bash
cp .env.example .env
```

Then edit `.env` and add both API keys:

```env
# Turbopuffer API key
TURBOPUFFER_API_KEY=your_actual_turbopuffer_api_key

# OpenRouter API key
OPENROUTER_API_KEY=your_actual_openrouter_api_key
```

### 5. Seed the Database

Load the movie dataset into Turbopuffer:

```bash
npm run seed
```

This command will:
1. Check if data already exists (to avoid duplicates)
2. Extract ~5,000 movies from the shared dataset at `/assets/movies_dataset_with_embeddings.csv.zip`
3. Upload movies with pre-computed embeddings to Turbopuffer
4. Take approximately 2-3 minutes to complete

**Note**: If you need to re-seed the database, delete the "movies" namespace in the Turbopuffer dashboard, then run the seed command again.

## Testing the Example

Once setup is complete, test the semantic search functionality using the MCP Inspector:

```bash
npm run dev
```

This opens an interactive playground where you can test the `search` tool. Try these example queries:

- **"space adventure movies"** - Finds sci-fi adventures like Star Wars
- **"romantic comedies from the 90s"** - Finds rom-coms by era and genre
- **"movies about artificial intelligence"** - Semantic understanding of themes
- **"films with strong female leads"** - Matches movies by character types
- **"action movies with car chases"** - Finds specific action subgenres

### Search Tool Parameters

The `search` tool accepts:
- `query` (required): Your natural language search query
- `limit` (optional): Maximum number of results (1-100, default: 10)

### Example Response

```json
{
  "query": "space adventure movies",
  "count": 10,
  "results": [
    {
      "id": 123,
      "title": "Star Wars",
      "overview": "A young farm boy joins a rebellion...",
      "release_date": "1977-05-25",
      "genre": "Science Fiction",
      "popularity": 98.5,
      "vote_average": 8.6,
      "similarity_score": 0.89
    }
    // ... more results
  ]
}
```

## How It Works

The semantic search system works in three steps:

1. **Query Embedding**: Your search query is sent to OpenRouter's embedding API ([sentence-transformers/paraphrase-minilm-l6-v2](https://huggingface.co/sentence-transformers/paraphrase-minilm-l6-v2)) which converts it into a 384-dimensional vector.

2. **Vector Similarity Search**: Turbopuffer's ANN (Approximate Nearest Neighbor) algorithm performs a cosine similarity search against the pre-computed movie embeddings.

3. **Results Ranking**: Movies are ranked by similarity score and returned with metadata (title, overview, genre, ratings, etc.).

### Why Semantic Search?

Traditional keyword search only matches exact words. Vector similarity search understands semantic meaning:
- "space adventure" matches "Star Wars" even though those exact words aren't in the description
- "romantic comedy" matches movies tagged as "Romance" or "Comedy"
- Searches work across different phrasings of the same concept

## Project Structure

```
.
├── scripts/
│   └── seed.ts          # Script to load and parse movie dataset
├── src/
│   ├── gram.ts          # Main Gram Function with search tool
│   └── server.ts        # MCP server setup
├── .env.example         # Environment variable template
├── package.json         # Dependencies and scripts
└── tsconfig.json        # TypeScript configuration
```

**Note:** The movie dataset (~5,000 movies with pre-computed embeddings) is stored in `/assets/movies_dataset_with_embeddings.csv.zip` at the project root to enable sharing across multiple examples.

### Key Files

**`src/gram.ts`**: Defines the `search` tool that:
- Generates embeddings for search queries using OpenRouter
- Queries Turbopuffer using vector similarity search
- Returns ranked results with similarity scores

**`scripts/seed.ts`**: Parses the CSV dataset and uploads movies with their embeddings to Turbopuffer's "movies" namespace.

## Deploying to Gram

To deploy this function to Gram and make it available as an MCP server:

```bash
# Build the deployment package
npm run build

# Push to Gram
npm run push
```

After deploying, users can install your MCP server and use the semantic search tool in any LLM that supports the Model Context Protocol.

## Additional Resources

- [Gram Functions Documentation](https://www.speakeasy.com/docs/gram/gram-functions)
- [Turbopuffer Documentation](https://turbopuffer.com/docs)
- [OpenRouter API Documentation](https://openrouter.ai/docs)
- [Model Context Protocol](https://modelcontextprotocol.io/)

## Learn More

To learn more about building Gram Functions, check out:
- [CONTRIBUTING.md](./CONTRIBUTING.md) - Framework usage guide
- [CLAUDE.md](./CLAUDE.md) - Development guidelines
