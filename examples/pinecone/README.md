# Semantic Movie Search with Pinecone Local

This example demonstrates how to build a **semantic search** system using [Gram Functions](https://getgram.ai), Pinecone Local (Docker-based vector database), and OpenRouter's embedding API. Users can search for movies using natural language queries, and the system returns semantically similar results based on vector embeddings.

## What This Example Demonstrates

- **Vector similarity search** using Pinecone Local (in-memory vector database)
- **Semantic search** that understands meaning, not just keywords (e.g., "space adventure" matches "Star Wars")
- **Real-time embedding generation** using OpenRouter's API
- **Gram Functions framework** for building LLM-compatible tools
- **Docker-based local development** with automatic seeding

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
# Pinecone API key
# For local development, this is optional (Pinecone Local doesn't require auth)
PINECONE_API_KEY=your_api_key_here

# OpenRouter API key for generating embeddings
# Get your key at: https://openrouter.ai/
OPENROUTER_API_KEY=your_api_key_here
```

### 3. Start Pinecone Local and Load Movie Data

Run the following command to:
1. Start Pinecone Local in Docker
2. Create the movies index
3. Seed the database with ~5,000 movies (including pre-computed embeddings)

```bash
npm run db:start
```

This command will automatically wait for Pinecone Local to be ready and then seed it with movie data from the shared dataset at `/../../assets/pgvector/dataset_with_embeddings.csv.zip`.

**Other database commands:**
- `npm run db:stop` - Stop the Pinecone Local container
- `npm run db:reset` - Reset everything (stops container, restarts fresh)
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
      "id": "movie_123",
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

2. **Vector Similarity Search**: Pinecone performs a cosine similarity search against the pre-computed movie embeddings stored in the index.

3. **Results Ranking**: Movies are ranked by similarity score (0-1, where 1 is most similar) and returned with metadata (title, overview, genre, ratings, etc.).

### Why Pinecone?

Pinecone is a purpose-built vector database designed for high-performance similarity search:
- **Fast queries**: Optimized for vector operations at scale
- **Easy to use**: Simple API for upsert and query operations
- **Local development**: Pinecone Local allows testing without cloud costs
- **Production ready**: Same API works for local and cloud deployment

Traditional keyword search only matches exact words. Vector similarity search understands semantic meaning:
- "space adventure" matches "Star Wars" even though those exact words aren't in the description
- "romantic comedy" matches movies tagged as "Romance" or "Comedy"
- Searches work across different phrasings of the same concept

## Project Structure

```
.
├── scripts/
│   └── seed.ts          # Script to create index and load movie dataset
├── src/
│   ├── gram.ts          # Main Gram Function with search tool
│   └── server.ts        # MCP server setup
├── docker-compose.yml   # Pinecone Local Docker setup
├── .env.example         # Environment variable template
├── package.json         # Dependencies and scripts
└── tsconfig.json        # TypeScript configuration
```

**Note:** The movie dataset (~5,000 movies with pre-computed embeddings) is stored in `/../../assets/pgvector/dataset_with_embeddings.csv.zip` to enable sharing across multiple examples.

### Key Files

**`src/gram.ts`**: Defines the `search` tool that:
- Generates embeddings for search queries using OpenRouter
- Queries Pinecone using vector similarity search
- Returns ranked results with similarity scores

**`scripts/seed.ts`**:
- Creates the Pinecone index if it doesn't exist
- Parses the CSV dataset
- Batch upserts movies with their embeddings (200 per batch)
- Stores all movie metadata in Pinecone

**`docker-compose.yml`**: Sets up Pinecone Local container on port 5081 using the `pinecone-index` image

## Pinecone Local vs Cloud

### Pinecone Local (Development)
- **Free**: No API costs during development
- **In-memory**: Data is lost when container stops
- **Limited**: Up to 100,000 records
- **Fast setup**: No account or API key required
- **Endpoint**: `http://localhost:5081`

### Pinecone Cloud (Production)
- **Scalable**: Handles billions of vectors
- **Persistent**: Data is stored reliably
- **Global**: Deploy close to your users
- **Requires**: API key from [pinecone.io](https://www.pinecone.io/)

To transition from local to cloud:
1. Sign up at [pinecone.io](https://www.pinecone.io/) and get your API key
2. Create a serverless index in the Pinecone console with:
   - Name: `movies`
   - Dimensions: 384
   - Metric: cosine
3. Update `.env` with your real `PINECONE_API_KEY`
4. Remove the index host parameter from `src/gram.ts` (change `pc.index("movies", "http://localhost:5081")` to `pc.index("movies")`)
5. Update `scripts/seed.ts` similarly to remove the host parameter
6. Re-run the seed script to populate your cloud index

## Deploying to Gram

To deploy this function to Gram and make it available as an MCP server:

```bash
# Build the deployment package
npm run build

# Push to Gram
npm run push
```

After deploying, users can install your MCP server and use the semantic search tool in any LLM that supports the Model Context Protocol.

**Note:** When deploying to production, ensure you're using a cloud Pinecone instance (not Pinecone Local) and update the connection configuration accordingly.

## Additional Resources

- [Gram Functions Documentation](https://www.speakeasy.com/docs/gram/gram-functions)
- [Pinecone Documentation](https://docs.pinecone.io/)
- [Pinecone Local Guide](https://docs.pinecone.io/guides/operations/local-development)
- [OpenRouter API Documentation](https://openrouter.ai/docs)
- [Model Context Protocol](https://modelcontextprotocol.io/)

## Learn More

To learn more about building Gram Functions, check out:
- [CONTRIBUTING.md](./CONTRIBUTING.md) - Framework usage guide
- [CLAUDE.md](./CLAUDE.md) - Development guidelines
