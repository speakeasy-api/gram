package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	chromem "github.com/philippgille/chromem-go"
)

const (
	vectorCollectionPrefix     = "toolset:"
	vectorMetadataPayloadKey   = "payload"
	vectorIndexConcurrency     = 5
	defaultFindToolsResultSize = 5
)

var errToolVectorCollectionNotFound = errors.New("toolset vector collection not found")

type toolVectorStore struct {
	mu sync.RWMutex
	db *chromem.DB
}

// in memory vector DB dependency
func newToolVectorStore() *toolVectorStore {
	return &toolVectorStore{
		db: chromem.NewDB(),
		mu: sync.RWMutex{},
	}
}

func (s *toolVectorStore) IndexToolset(ctx context.Context, toolsetID string, entries []*toolListEntry) error {
	if s == nil {
		return errors.New("tool vector store is not initialized")
	}
	if toolsetID == "" {
		return errors.New("toolset id is required")
	}

	collectionName := collectionNameForToolset(toolsetID)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Drop existing collection contents to ensure we have a fresh snapshot.
	if err := s.db.DeleteCollection(collectionName); err != nil {
		return fmt.Errorf("delete collection %s: %w", collectionName, err)
	}

	collection, err := s.db.CreateCollection(collectionName, map[string]string{
		"toolset_id": toolsetID,
	}, nil)
	if err != nil {
		return fmt.Errorf("create collection %s: %w", collectionName, err)
	}

	docs := make([]chromem.Document, 0, len(entries))
	for _, entry := range entries {
		if entry == nil || entry.Name == "" {
			continue
		}

		payload, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("marshal tool entry %s: %w", entry.Name, err)
		}

		docs = append(docs, chromem.Document{ //nolint:exhaustruct // embedding is automatic
			ID: fmt.Sprintf("%s:%s", collectionName, entry.Name),
			Metadata: map[string]string{
				vectorMetadataPayloadKey: string(payload),
				"name":                   entry.Name,
			},
			Content: buildEmbeddableContent(entry),
		})
	}

	if len(docs) == 0 {
		return nil
	}

	if err := collection.AddDocuments(ctx, docs, vectorIndexConcurrency); err != nil {
		return fmt.Errorf("error adding tools to index %s: %w", collectionName, err)
	}

	return nil
}

func (s *toolVectorStore) SearchToolset(ctx context.Context, toolsetID, query string, limit int) ([]*toolListEntry, error) {
	if s == nil {
		return nil, errors.New("tool vector store is not initialized")
	}
	if toolsetID == "" {
		return nil, errors.New("toolset id is required")
	}
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("query is required")
	}
	if limit <= 0 {
		limit = defaultFindToolsResultSize
	}

	collectionName := collectionNameForToolset(toolsetID)

	s.mu.RLock()
	collection := s.db.GetCollection(collectionName, nil)
	s.mu.RUnlock()

	if collection == nil {
		return nil, errToolVectorCollectionNotFound
	}

	docCount := collection.Count()
	if docCount == 0 {
		return nil, nil
	}
	if limit > docCount {
		limit = docCount
	}

	results, err := collection.Query(ctx, query, limit, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("query collection %s: %w", collectionName, err)
	}

	matches := make([]*toolListEntry, 0, len(results))
	for _, res := range results {
		var payload string
		if res.Metadata != nil {
			payload = res.Metadata[vectorMetadataPayloadKey]
		}
		if payload == "" {
			continue
		}

		var entry toolListEntry
		if err := json.Unmarshal([]byte(payload), &entry); err != nil {
			return nil, fmt.Errorf("unmarshal tool entry payload: %w", err)
		}

		// Preserve similarity for potential future use in responses.
		entry.Meta = ensureMetaMap(entry.Meta)
		entry.Meta["similarity_score"] = res.Similarity
		matches = append(matches, &entry)
	}

	return matches, nil
}

func ensureMetaMap(meta map[string]any) map[string]any {
	if meta == nil {
		return map[string]any{}
	}
	return meta
}

func collectionNameForToolset(toolsetID string) string {
	return vectorCollectionPrefix + toolsetID
}

func buildEmbeddableContent(entry *toolListEntry) string {
	var schema string
	if len(entry.InputSchema) > 0 {
		schema = string(entry.InputSchema)
	}

	var meta string
	if entry.Meta != nil {
		if payload, err := json.Marshal(entry.Meta); err == nil {
			meta = string(payload)
		}
	}

	parts := []string{
		entry.Name,
		entry.Description,
		schema,
		meta,
	}

	return strings.TrimSpace(strings.Join(filterNonEmpty(parts), "\n"))
}

func filterNonEmpty(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			filtered = append(filtered, v)
		}
	}
	return filtered
}
