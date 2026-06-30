package bq

import (
	"context"
	"fmt"

	"cloud.google.com/go/bigquery"
)

type InserterOptions struct {
	// IgnoreUnknownValues causes values not matching the schema to be ignored.
	// The default value is false, which causes records containing such values
	// to be treated as invalid records.
	IgnoreUnknownValues bool
	// SkipInvalidRows causes rows containing invalid data to be silently
	// ignored. The default value is false, which causes the entire request to
	// fail if there is an attempt to insert an invalid row.
	SkipInvalidRows bool
}

type Client interface {
	Dataset(datasetID string) DatasetHandle
}

type DatasetHandle interface {
	Table(tableID string) TableHandle
}

type TableHandle interface {
	Inserter(options InserterOptions) Inserter
}

type Inserter interface {
	Put(ctx context.Context, items any) error
}

type wrappedClient struct {
	c *bigquery.Client
}

func NewClient(c *bigquery.Client) Client {
	return &wrappedClient{c: c}
}

func (c *wrappedClient) Dataset(datasetID string) DatasetHandle {
	return &wrappedDatasetHandle{d: c.c.Dataset(datasetID)}
}

type wrappedDatasetHandle struct {
	d *bigquery.Dataset
}

func (d *wrappedDatasetHandle) Table(tableID string) TableHandle {
	return &wrappedTableHandle{t: d.d.Table(tableID)}
}

type wrappedTableHandle struct {
	t *bigquery.Table
}

func (t *wrappedTableHandle) Inserter(options InserterOptions) Inserter {
	ins := t.t.Inserter()
	ins.IgnoreUnknownValues = options.IgnoreUnknownValues
	ins.SkipInvalidRows = options.SkipInvalidRows
	return &wrappedInserter{i: ins}
}

type wrappedInserter struct {
	i *bigquery.Inserter
}

func (i *wrappedInserter) Put(ctx context.Context, items any) error {
	err := i.i.Put(ctx, items)
	if err != nil {
		return fmt.Errorf("wrapped client put: %w", err)
	}

	return nil
}

type noopClient struct{}

func NewNoopClient() Client {
	return &noopClient{}
}

func (c *noopClient) Dataset(datasetID string) DatasetHandle {
	return &noopDatasetHandle{}
}

type noopDatasetHandle struct{}

func (d *noopDatasetHandle) Table(tableID string) TableHandle {
	return &noopTableHandle{}
}

type noopTableHandle struct{}

func (t *noopTableHandle) Inserter(InserterOptions) Inserter {
	return &noopInserter{}
}

type noopInserter struct{}

func (i *noopInserter) Put(ctx context.Context, items any) error {
	return nil
}
