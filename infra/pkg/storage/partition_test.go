package storage

import (
	"strings"
	"testing"
	"time"

	pubsubv1 "github.com/speakeasy-api/gram/infra/gen/gcp/pubsub/v1"
	"github.com/stretchr/testify/require"
)

func TestPartition_StrictExternalGrammar(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		value  string
		reason DropReason
	}{
		{"region=eu-west1/account=001", DropNone},
		{"region=us.east-1/account=_abc-123", DropNone},
		{"region=a/account=" + strings.Repeat("a", 128), DropNone},
		{"", DropMalformed}, {"region=a/account=", DropMalformed},
		{"account=1/region=a", DropMalformed}, {"region=a/region=b", DropMalformed},
		{"region=a/account=b/other=c", DropMalformed},
		{"region=a/account=../escape", DropMalformed}, {"region=a/account=..", DropMalformed},
		{"region=a/account=a%2Fb", DropMalformed}, {"region=a/account=a\\b", DropMalformed},
		{"region=a/account=b=c", DropMalformed}, {"region=a/account=naïve", DropMalformed},
		{"region=a/account=a b", DropMalformed}, {"region=a/account=a\n", DropMalformed},
		{"region=a/account=null", DropMalformed}, {"region=a/account=__Hive_Default_Partition__", DropMalformed},
		{"region=a/account=" + strings.Repeat("a", 129), DropLimit}, {strings.Repeat("a", 513), DropLimit},
	} {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()

			def := Definition{Partitioning: pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_EXTERNAL, PartitionAttribute: "partition", PartitionKeys: []string{"region", "account"}}
			got, reason := partition(def, map[string]string{"partition": tt.value}, time.Time{})
			require.Equal(t, tt.reason, reason)
			if reason == DropNone {
				require.Equal(t, tt.value, got)
			} else {
				require.Empty(t, got)
			}
		})
	}
}

func TestPartition_MissingExternalAttribute(t *testing.T) {
	t.Parallel()

	def := Definition{Partitioning: pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_EXTERNAL, PartitionAttribute: "partition", PartitionKeys: []string{"region"}}
	got, reason := partition(def, nil, time.Time{})
	require.Equal(t, DropMissing, reason)
	require.Empty(t, got)
}

func TestPartition_IngestionUsesUTC(t *testing.T) {
	t.Parallel()

	received := time.Date(2026, 10, 7, 23, 30, 0, 0, time.FixedZone("local", -7*60*60))
	for _, tt := range []struct {
		mode   pubsubv1.StoragePartitioning
		suffix string
	}{
		{pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_DAILY, "part__year=2026/part__month=10/part__day=08"},
		{pubsubv1.StoragePartitioning_STORAGE_PARTITIONING_HIVE_HOURLY, "part__year=2026/part__month=10/part__day=08/part__hour=06"},
	} {
		got, reason := partition(Definition{Partitioning: tt.mode}, nil, received)
		require.Equal(t, DropNone, reason)
		require.Equal(t, tt.suffix, got)
	}
}

func TestParseBucketMapping(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{`null`, `[]`, `{"archive":"gs://bucket"}`, `{"archive":"short" , "another":"short"}`, `{"bad/name":"123-bucket"}`, `{"archive":"goog-reserved"}`,
		`{"archive":"first-bucket","archive":"second-bucket"}`, `{"archive":"first-bucket","\u0061rchive":"second-bucket"}`,
		`{"archive":"123-google-data"}`, `{"archive":"123-g00gle-data"}`, `{"archive":"123-go0gle-data"}`, `{"archive":"123-g0ogle-data"}`,
		`{"archive":"123-bucket"} {}`, `{"archive":"123-bucket"`, `{"archive":null}`, `{"archive":42}`,
	} {
		_, err := ParseBucketMapping(raw)
		require.Error(t, err, raw)
	}

	mapping, err := ParseBucketMapping(`{"event-archive":"123-dev-event-archive"}`)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"event-archive": "123-dev-event-archive"}, mapping)
}
