# ClickHouse Gram Function

This Gram Function provides a tool for executing SQL queries against a ClickHouse database using the `@clickhouse/client-web` driver for serverless environments.

## Overview

This example demonstrates how to:
- Connect to a ClickHouse database using the web client
- Execute parameterized SQL queries with ClickHouse's named parameter syntax
- Work with time series data (1M+ TrackPlays rows)
- Query the complete Chinook sample dataset

The function provides a single tool:
- `execute_query`: Execute SQL queries against your ClickHouse database with named parameters

## Prerequisites

1. Docker and Docker Compose for local development
2. Node.js v22+ and npm
3. The Chinook dataset (included in `../../assets/chinook_dataset.csv.zip`)

## Setting up the ClickHouse Database

### Quick Setup (Recommended)

Start ClickHouse, wait for it to be ready, and load the dataset with a single command:

```bash
npm run db:start
```

This will:
1. Start the ClickHouse Docker container (port 8124)
2. Wait for ClickHouse to be ready
3. Automatically seed the database with the Chinook dataset

### Database Management Scripts

Convenient npm scripts for managing your ClickHouse instance:

```bash
# Start ClickHouse and seed database
npm run db:start

# Stop ClickHouse (preserves data)
npm run db:stop

# Reset database (removes all data and reseeds)
npm run db:reset

# Seed/reseed the database only
npm run db:seed
```

### Manual Setup

If you prefer to run commands manually:

```bash
# Start ClickHouse
docker compose up -d

# Wait for it to be ready
npm run db:wait

# Seed the database
npm run db:seed
```

The ClickHouse container runs with:
- HTTP interface on port 8124
- Database: `gram_example`
- Username: `gram_user`
- Password: `gram_password`

### The Dataset

The Chinook dataset includes music metadata and 1M+ time series track play events:

- **TrackPlays** - 1M+ time series play events (partitioned by month)
- **Track** - 3.5K tracks with metadata
- **Album** - Albums by artists
- **Artist** - Music artists
- **Genre** - Music genres
- **MediaType** - Media file types
- **Playlist** / **PlaylistTrack** - User playlists
- **Customer** / **Employee** / **Invoice** / **InvoiceLine** - Store data

**Note:** The seed script will skip seeding if data already exists. Use `npm run db:reset` to wipe and reload data.

## Configuration

The function requires these environment variables:

- `CLICKHOUSE_HOST` - ClickHouse server host (e.g., `localhost`)
- `CLICKHOUSE_PORT` - HTTP port (e.g., `8124`)
- `CLICKHOUSE_DATABASE` - Database name (e.g., `gram_example`)
- `CLICKHOUSE_USERNAME` - Username (e.g., `gram_user`)
- `CLICKHOUSE_PASSWORD` - Database password

For local development with Docker, use the values shown in the seeding command above.

## Quick Start

Install dependencies:

```bash
npm install
```

Start ClickHouse and load data:

```bash
npm run db:start
```

To test locally with MCP inspector:

```bash
npm run dev
```

This automatically starts ClickHouse (if not running) and spins up [MCP inspector][mcp-inspector] to let you interactively test your tools.

## Testing

The project includes a comprehensive test suite with 7 tests covering:
- Simple SELECT queries
- Parameterized queries with named parameters
- Time series aggregations
- JOIN queries across multiple tables
- UUID filtering
- Genre popularity analysis by month
- User listening pattern analysis

### Running Tests

First, ensure ClickHouse is running and seeded:

```bash
npm run db:start
```

Then run the test suite with environment variables:

```bash
CLICKHOUSE_HOST=localhost \
CLICKHOUSE_PORT=8124 \
CLICKHOUSE_USERNAME=gram_user \
CLICKHOUSE_PASSWORD=gram_password \
CLICKHOUSE_DATABASE=gram_example \
npm test
```

Or run tests in watch mode during development:

```bash
CLICKHOUSE_HOST=localhost \
CLICKHOUSE_PORT=8124 \
CLICKHOUSE_USERNAME=gram_user \
CLICKHOUSE_PASSWORD=gram_password \
CLICKHOUSE_DATABASE=gram_example \
npm test -- --watch
```

### Test Coverage

The tests verify:
- ✅ Connection to ClickHouse and database accessibility
- ✅ 1M+ TrackPlays rows are loaded
- ✅ Basic query execution returns correct data
- ✅ Named parameter substitution works correctly
- ✅ Time series aggregations (COUNT, DISTINCT, GROUP BY)
- ✅ Multi-table JOINs with Track, Album, Artist, Genre
- ✅ UUID data type handling
- ✅ Date range filtering and date functions

## Example Queries

ClickHouse uses **named parameters** with `{param_name: Type}` syntax (not PostgreSQL's `$1, $2`):

### Daily play counts
```sql
SELECT Date, COUNT(*) as plays
FROM TrackPlays
WHERE Date >= {start_date: Date} AND Date <= {end_date: Date}
GROUP BY Date
ORDER BY Date;
```

Parameters: `{ start_date: "2023-01-01", end_date: "2023-01-31" }`

### Top 10 most played tracks with details
```sql
SELECT
  t.Name as TrackName,
  ar.Name as ArtistName,
  al.Title as AlbumName,
  COUNT(*) as PlayCount
FROM TrackPlays tp
JOIN Track t ON tp.TrackId = t.TrackId
JOIN Album al ON t.AlbumId = al.AlbumId
JOIN Artist ar ON al.ArtistId = ar.ArtistId
WHERE tp.Date >= {start_date: Date}
GROUP BY t.TrackId, t.Name, ar.Name, al.Title
ORDER BY PlayCount DESC
LIMIT 10;
```

Parameters: `{ start_date: "2023-01-01" }`

### User listening patterns
```sql
SELECT
  UserId,
  COUNT(*) as total_plays,
  COUNT(DISTINCT TrackId) as unique_tracks,
  MIN(Date) as first_play,
  MAX(Date) as last_play
FROM TrackPlays
GROUP BY UserId
ORDER BY total_plays DESC
LIMIT {limit: UInt32};
```

Parameters: `{ limit: 100 }`

### Genre popularity by month
```sql
SELECT
  toYYYYMM(tp.Date) as month,
  g.Name as Genre,
  COUNT(*) as plays
FROM TrackPlays tp
JOIN Track t ON tp.TrackId = t.TrackId
JOIN Genre g ON t.GenreId = g.GenreId
GROUP BY month, g.GenreId, g.Name
ORDER BY month, plays DESC
LIMIT 20;
```

## ClickHouse SQL Features

This example demonstrates several ClickHouse-specific features:

### Time Series Optimization
- TrackPlays table uses `MergeTree` engine
- Partitioned by month: `PARTITION BY toYYYYMM(Date)`
- Ordered for query performance: `ORDER BY (Date, UserId, TrackId)`

### Named Parameters
Unlike PostgreSQL (`$1, $2`), ClickHouse uses:
```sql
WHERE Date = {target_date: Date}
AND TrackId = {track_id: UInt32}
```

### Date Functions
- `toYYYYMM(date)` - Extract year-month as integer
- `toStartOfMonth(date)` - First day of month
- `dateDiff('day', start, end)` - Date difference

### Aggregation
- `COUNT(DISTINCT column)` - Unique values
- `uniq(column)` - Approximate unique count (faster)
- `groupArray(column)` - Array of values

## Docker Management

Use the convenient npm scripts:

```bash
# Start ClickHouse and seed database
npm run db:start

# Stop ClickHouse (preserves data)
npm run db:stop

# Reset database (removes all data and reseeds)
npm run db:reset
```

Or use Docker Compose directly:

```bash
# View logs
docker compose logs -f clickhouse

# Restart container
docker compose restart clickhouse

# Check container status
docker compose ps
```

## Learn More

To learn more about using the Gram Functions framework, check out [CONTRIBUTING.md](./CONTRIBUTING.md).

For ClickHouse documentation:
- [ClickHouse SQL Reference](https://clickhouse.com/docs/sql-reference)
- [JavaScript Client](https://clickhouse.com/docs/integrations/javascript)
- [Time Series Functions](https://clickhouse.com/docs/sql-reference/functions/date-time-functions)

[mcp-inspector]: https://github.com/modelcontextprotocol/inspector
