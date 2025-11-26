-- Enable pgvector extension
CREATE EXTENSION IF NOT EXISTS vector;

-- Create movies table with vector support
CREATE TABLE IF NOT EXISTS movies (
  id SERIAL PRIMARY KEY,
  release_date DATE,
  title TEXT NOT NULL,
  overview TEXT,
  popularity FLOAT,
  vote_count INTEGER,
  vote_average FLOAT,
  original_language VARCHAR(10),
  genre TEXT,
  poster_url TEXT,
  embedding vector(384), -- OpenAI embeddings are 384 dimensions
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Create index for vector similarity search
CREATE INDEX IF NOT EXISTS movies_embedding_idx ON movies USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);

-- Create index on title for text search
CREATE INDEX IF NOT EXISTS movies_title_idx ON movies (title);

-- Create index on release_date for filtering
CREATE INDEX IF NOT EXISTS movies_release_date_idx ON movies (release_date);
