# PlanetScale Gram Function

This Gram Function provides a tool for executing SQL queries against a PlanetScale PostgreSQL database using their serverless HTTP driver.

## Overview

This example demonstrates how to:
- Connect to a PlanetScale database using the serverless driver
- Execute parameterized SQL queries
- Work with the Chinook sample dataset

The function provides a single tool:
- `execute_query`: Execute SQL queries against your PlanetScale database

## Prerequisites

1. A PlanetScale account with a PostgreSQL database
2. Database credentials (host, username, password)
3. The Chinook dataset loaded into your database (see below)

## Setting up the Chinook Dataset

The Chinook dataset is a sample database representing a digital media store. It includes tables for artists, albums, tracks, customers, invoices, and more.

### Loading the Dataset

The easiest way to load the Chinook dataset is to use the provided seed script:

```bash
export PLANETSCALE_HOST="your-host.pg.psdb.cloud"
export PLANETSCALE_PORT="6432"
export PLANETSCALE_DATABASE="your-database-name"
export PLANETSCALE_USERNAME="your-username"
export PLANETSCALE_PASSWORD="pscale_pw_XXXX"
npm run db:seed
```

This script will:
1. Extract the CSV files from `../../assets/chinook_dataset.csv.zip`
2. Create all necessary tables in your PlanetScale database
3. Import the data with proper foreign key relationships
4. Clean up temporary files

The Chinook dataset includes these tables:
- `Artist` - Music artists
- `Album` - Albums by artists
- `Track` - Individual tracks on albums
- `MediaType` - Types of media files
- `Genre` - Music genres
- `Playlist` - User playlists
- `PlaylistTrack` - Tracks in playlists
- `Customer` - Store customers
- `Employee` - Store employees
- `Invoice` - Customer invoices
- `InvoiceLine` - Line items on invoices

**Note:** The seed script will skip seeding if data already exists. To re-seed, you'll need to drop the tables first.

## Configuration

The function requires these environment variables:

- `PLANETSCALE_HOST` - Your PlanetScale PostgreSQL host (e.g., `your-db.pg.psdb.cloud`)
- `PLANETSCALE_PORT` - The port number (default: `6432`)
- `PLANETSCALE_DATABASE` - Your database name
- `PLANETSCALE_USERNAME` - Your PostgreSQL username
- `PLANETSCALE_PASSWORD` - Your database password (format: `pscale_pw_XXXX`)

These are configured when you install the MCP server in Gram.

## Quick Start

Install dependencies:

```bash
npm install
```

To build a zip file that can be deployed to Gram:

```bash
npm run build
```

After building, push your function to Gram:

```bash
npm push
```

## Testing Locally

Start a local MCP server with the MCP inspector:

```bash
npm run dev
```

This spins up [MCP inspector][mcp-inspector] to let you interactively test your tools.

You'll need to set the environment variables before running:

```bash
export PLANETSCALE_HOST="your-host.pg.psdb.cloud"
export PLANETSCALE_PORT="6432"
export PLANETSCALE_DATABASE="your-database-name"
export PLANETSCALE_USERNAME="your-username"
export PLANETSCALE_PASSWORD="pscale_pw_XXXX"
npm run dev
```

## Example Queries

Once deployed, you can use the `execute_query` tool to run queries like:

```sql
-- Get all artists
SELECT * FROM Artist LIMIT 10;

-- Find albums by a specific artist (using parameterized query)
SELECT a.Title, ar.Name
FROM Album a
JOIN Artist ar ON a.ArtistId = ar.ArtistId
WHERE ar.Name = $1;

-- Get top 5 customers by total purchases
SELECT c.FirstName, c.LastName, SUM(i.Total) as TotalSpent
FROM Customer c
JOIN Invoice i ON c.CustomerId = i.CustomerId
GROUP BY c.CustomerId
ORDER BY TotalSpent DESC
LIMIT 5;
```

**Note:** This example uses PlanetScale's PostgreSQL product with the Neon serverless driver. Queries use PostgreSQL-style parameterized queries with `$1, $2, etc.` placeholders.

## Learn More

To learn more about using the Gram Functions framework, check out [CONTRIBUTING.md](./CONTRIBUTING.md).

[mcp-inspector]: https://github.com/modelcontextprotocol/inspector
