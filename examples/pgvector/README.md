# Semantic Movie Search with pgvector

This example demonstrates how to build a **semantic search** system using [Gram
Functions](https://getgram.ai), PostgreSQL with the
[pgvector](https://github.com/pgvector/pgvector) extension, and OpenRouter's
embedding API. Users can search for movies using natural language queries, and
the system returns semantically similar results based on vector embeddings.

## What This Example Demonstrates

- **Vector similarity search** using PostgreSQL with pgvector extension
- **Semantic search** that understands meaning, not just keywords (e.g., "space adventure" matches "Star Wars")
- **Real-time embedding generation** using OpenRouter's API
- **Gram Functions framework** for building LLM-compatible tools
- **Docker-based database setup** with automatic seeding

## Prerequisites

Before running this example, you'll need:

- **Docker** and **Docker Compose** installed ([Get Docker](https://docs.docker.com/get-docker/))
- **Node.js v22** or later ([Download Node.js](https://nodejs.org/))
- **npm** (comes with Node.js)
- **OpenRouter API key** ([Sign up at OpenRouter](https://openrouter.ai/))

## Setup Instructions

### 1. Install Dependencies

```bash
npm install
```

### 2. Configure Environment Variables

Create a `.env` file based on the provided example:

```bash
cp .env.example .env
```

Then edit `.env` and add your OpenRouter API key:

```env
# PostgreSQL connection string (default works for local Docker setup)
DATABASE_URL=postgresql://postgres:postgres@localhost:5432/movies

# OpenRouter API key for generating embeddings
# Get your key at: https://openrouter.ai/
OPENROUTER_API_KEY=your_api_key_here
```

### 3. Start Database and Load Movie Data

Run the following command to:
1. Start PostgreSQL with pgvector in Docker
2. Create the database schema
3. Seed the database with ~5,000 movies (including pre-computed embeddings)

```bash
npm run db:start
```

This command will automatically wait for the database to be ready and then seed it with movie data from `dataset_with_embeddings.csv.zip`.

**Other database commands:**
- `npm run db:stop` - Stop the database container
- `npm run db:reset` - Reset everything (stops, removes data, restarts fresh)
- `npm run db:seed` - Manually re-run the seed script

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
    },
    // ... more results
  ]
}
```

## How It Works

The semantic search system works in three steps:

1. **Query Embedding**: Your search query is sent to OpenRouter's embedding API ([sentence-transformers/paraphrase-minilm-l6-v2](https://huggingface.co/sentence-transformers/paraphrase-minilm-l6-v2)) which converts it into a 384-dimensional vector.

2. **Vector Similarity Search**: PostgreSQL's pgvector extension performs a cosine similarity search against the pre-computed movie embeddings in the database.

3. **Results Ranking**: Movies are ranked by similarity score and returned with metadata (title, overview, genre, ratings, etc.).

### Why pgvector?

Traditional keyword search only matches exact words. Vector similarity search understands semantic meaning:
- "space adventure" matches "Star Wars" even though those exact words aren't in the description
- "romantic comedy" matches movies tagged as "Romance" or "Comedy"
- Searches work across different phrasings of the same concept

## Project Structure

```
.
├── scripts/
│   ├── seed.ts          # Script to load and parse movie dataset
├── src/
│   ├── gram.ts          # Main Gram Function with search tool
│   └── server.ts        # MCP server setup
├── init.sql             # Database schema with pgvector extension
├── dataset_with_embeddings.csv.zip  # ~5,000 movies with pre-computed embeddings
├── docker-compose.yml   # PostgreSQL with pgvector setup
├── .env.example         # Environment variable template
├── package.json         # Dependencies and scripts
└── tsconfig.json        # TypeScript configuration
```

### Key Files

**`src/gram.ts`**: Defines the `search` tool that:
- Generates embeddings for search queries using OpenRouter
- Queries PostgreSQL using pgvector's cosine distance operator (`<->`)
- Returns ranked results with similarity scores

**`init.sql`**: Sets up:
- The `vector` extension for PostgreSQL
- A `movies` table with a `vector(384)` column for embeddings
- An IVFFlat index for fast similarity search

**`seed.ts`**: Parses the CSV dataset and inserts movies with their embeddings into the database in batches.

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
- [pgvector GitHub Repository](https://github.com/pgvector/pgvector)
- [OpenRouter API Documentation](https://openrouter.ai/docs)
- [Model Context Protocol](https://modelcontextprotocol.io/)

## Learn More

To learn more about building Gram Functions, check out:
- [CONTRIBUTING.md](./CONTRIBUTING.md) - Framework usage guide
- [CLAUDE.md](./CLAUDE.md) - Development guidelines
