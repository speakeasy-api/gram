package gcp

import (
	"cmp"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ettle/strcase"
	pubsubv1 "github.com/speakeasy-api/gram/infra/gen/gcp/pubsub/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

const (
	// defaultPartitionExpiration is applied to a BigQuery sink's monthly
	// ingestion-time partitions when the marker leaves partition_expiration
	// unset. An explicit 0s disables expiration entirely.
	defaultPartitionExpiration = 60 * 24 * time.Hour

	// maxBigQueryIDLen bounds dataset and table IDs. BigQuery allows up to 1024
	// characters for each.
	maxBigQueryIDLen = 1024
)

// timestampFullName is the proto full name of the well-known timestamp type,
// which maps to a BigQuery TIMESTAMP column rather than a nested RECORD.
const timestampFullName = "google.protobuf.Timestamp"

// DesiredBigQueryDataset is a BigQuery dataset derived from the proto package of
// one or more BigQuery sinks. Datasets are shared across the sinks in a package,
// so they carry no per-message proto label.
type DesiredBigQueryDataset struct {
	Name   string
	Labels map[string]string
}

// DesiredBigQueryTable is a BigQuery table that a BigQuery sink subscription
// writes into. Its schema is derived from the topic message the sink consumes;
// its partitioning is monthly on ingestion time with an optional expiration.
type DesiredBigQueryTable struct {
	Name    string
	Dataset string
	// Schema is the JSON-encoded BigQuery table schema (an array of column
	// definitions) derived from the topic message.
	Schema string
	// ProtoMessage is the sink marker's full name (the declaration), used for the
	// traceability label. The schema itself comes from the topic message.
	ProtoMessage string
	// PartitionExpiration is the monthly-partition retention. Only applied when
	// PartitionExpirationEnabled is true; when false, partitions never expire.
	PartitionExpiration        time.Duration
	PartitionExpirationEnabled bool
	Labels                     map[string]string
}

// bigQueryDatasetID derives a BigQuery dataset ID from a marker message's proto
// package: dots become underscores (e.g. gram.risk.v1 → gram_risk_v1). BigQuery
// dataset IDs may contain only letters, numbers, and underscores.
func bigQueryDatasetID(message protoreflect.MessageDescriptor) string {
	pkg := string(message.ParentFile().Package())
	return strings.ReplaceAll(pkg, ".", "_")
}

// bigQueryTableID derives a BigQuery table ID from a marker message's short
// name, snake-cased (e.g. FindingSink → finding_sink).
func bigQueryTableID(message protoreflect.MessageDescriptor) string {
	return strcase.ToSnake(string(message.Name()))
}

// resolvePartitionExpiration interprets a sink's partition_expiration: unset
// (nil) defaults to 60 days; an explicit 0s disables expiration; any positive
// value is used as-is. Returns the duration and whether expiration applies.
func resolvePartitionExpiration(bq *pubsubv1.BigQuerySinkOptions) (time.Duration, bool) {
	pe := bq.GetPartitionExpiration()
	switch {
	case pe == nil:
		return defaultPartitionExpiration, true
	case pe.AsDuration() == 0:
		return 0, false
	default:
		return pe.AsDuration(), true
	}
}

// DiscoverBigQuery walks the descriptor set and, for every subscription marker
// declaring a `bigquery` sink, produces the BigQuery dataset and table that back
// it. The table schema is derived from the topic message the sink consumes (its
// full name resolved against the descriptor set), so an unknown topic reference
// fails generation here just as it does for ordinary subscriptions.
func DiscoverBigQuery(descriptorBytes []byte) ([]DesiredBigQueryDataset, []DesiredBigQueryTable, error) {
	var descriptorSet descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(descriptorBytes, &descriptorSet); err != nil {
		return nil, nil, fmt.Errorf("unmarshal descriptor set: %w", err)
	}

	files, err := protodesc.NewFiles(&descriptorSet)
	if err != nil {
		return nil, nil, fmt.Errorf("build proto file registry: %w", err)
	}

	datasetByID := map[string]DesiredBigQueryDataset{}
	tableByKey := map[string]string{}
	tables := make([]DesiredBigQueryTable, 0)

	var walkErr error
	files.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		walkErr = collectBigQueryFromMessages(file.Messages(), files, datasetByID, tableByKey, &tables)
		return walkErr == nil
	})
	if walkErr != nil {
		return nil, nil, walkErr
	}

	datasets := make([]DesiredBigQueryDataset, 0, len(datasetByID))
	for _, dataset := range datasetByID {
		datasets = append(datasets, dataset)
	}

	return datasets, tables, nil
}

func collectBigQueryFromMessages(
	messages protoreflect.MessageDescriptors,
	files *protoregistry.Files,
	datasetByID map[string]DesiredBigQueryDataset,
	tableByKey map[string]string,
	tables *[]DesiredBigQueryTable,
) error {
	for i := 0; i < messages.Len(); i++ {
		message := messages.Get(i)

		if subOptions, ok := SubscriptionOptionsFromMessage(message); ok {
			if bq := subOptions.GetBigquery(); bq != nil {
				if err := collectBigQuerySink(message, subOptions, bq, files, datasetByID, tableByKey, tables); err != nil {
					return err
				}
			}
		}

		if err := collectBigQueryFromMessages(message.Messages(), files, datasetByID, tableByKey, tables); err != nil {
			return err
		}
	}
	return nil
}

func collectBigQuerySink(
	message protoreflect.MessageDescriptor,
	subOptions *pubsubv1.SubscriptionOptions,
	bq *pubsubv1.BigQuerySinkOptions,
	files *protoregistry.Files,
	datasetByID map[string]DesiredBigQueryDataset,
	tableByKey map[string]string,
	tables *[]DesiredBigQueryTable,
) error {
	topicName := strings.TrimSpace(subOptions.GetTopic())
	if topicName == "" {
		return fmt.Errorf("BigQuery sink %s is missing a topic reference", message.FullName())
	}

	desc, err := files.FindDescriptorByName(protoreflect.FullName(topicName))
	if err != nil {
		return fmt.Errorf("BigQuery sink %s references unknown topic message %q: %w", message.FullName(), topicName, err)
	}
	topicMessage, ok := desc.(protoreflect.MessageDescriptor)
	if !ok {
		return fmt.Errorf("BigQuery sink %s topic reference %q is not a message", message.FullName(), topicName)
	}
	if _, ok := TopicOptionsFromMessage(topicMessage); !ok {
		return fmt.Errorf("BigQuery sink %s references %q which does not declare a topic", message.FullName(), topicName)
	}

	schema, err := bigqueryTableSchema(topicMessage)
	if err != nil {
		return fmt.Errorf("derive BigQuery schema for sink %s from %s: %w", message.FullName(), topicName, err)
	}

	datasetID := bigQueryDatasetID(message)
	tableID := bigQueryTableID(message)

	if _, exists := datasetByID[datasetID]; !exists {
		datasetByID[datasetID] = DesiredBigQueryDataset{
			Name:   datasetID,
			Labels: map[string]string{"managed_by": managedByLabel},
		}
	}

	tableKey := datasetID + "." + tableID
	if existing, exists := tableByKey[tableKey]; exists {
		return fmt.Errorf("BigQuery table %q for sink %s collides with table declared on %s", tableKey, message.FullName(), existing)
	}
	tableByKey[tableKey] = string(message.FullName())

	labels := map[string]string{"managed_by": managedByLabel}
	if messageDeprecated(message) {
		labels[deprecatedLabelKey] = deprecatedLabelValue
	}

	expiration, expirationEnabled := resolvePartitionExpiration(bq)

	*tables = append(*tables, DesiredBigQueryTable{
		Name:                       tableID,
		Dataset:                    datasetID,
		Schema:                     schema,
		ProtoMessage:               string(message.FullName()),
		PartitionExpiration:        expiration,
		PartitionExpirationEnabled: expirationEnabled,
		Labels:                     labels,
	})

	return nil
}

// bqField is one BigQuery table column in the REST schema JSON shape. Nested
// RECORD columns carry their subfields under `fields`.
type bqField struct {
	Name   string    `json:"name"`
	Type   string    `json:"type"`
	Mode   string    `json:"mode"`
	Fields []bqField `json:"fields,omitempty"`
}

// bigqueryTableSchema derives the JSON-encoded BigQuery table schema from a
// protobuf message's fields. The JSON is indented so that, once marshaled into
// the values document, it renders as a readable multi-line YAML block scalar
// rather than one long quoted line. JSON whitespace is insignificant, so the
// schema BigQuery receives is unchanged.
func bigqueryTableSchema(message protoreflect.MessageDescriptor) (string, error) {
	fields, err := bigquerySchemaFields(message)
	if err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal BigQuery schema: %w", err)
	}
	return string(raw), nil
}

func bigquerySchemaFields(message protoreflect.MessageDescriptor) ([]bqField, error) {
	fds := message.Fields()

	// Order columns by proto field number rather than declaration order so the
	// generated schema is stable: reordering field declarations in the .proto
	// (without renumbering) leaves the schema — and the committed kcc.yaml —
	// unchanged.
	ordered := make([]protoreflect.FieldDescriptor, 0, fds.Len())
	for i := 0; i < fds.Len(); i++ {
		ordered = append(ordered, fds.Get(i))
	}
	slices.SortFunc(ordered, func(a, b protoreflect.FieldDescriptor) int {
		return cmp.Compare(a.Number(), b.Number())
	})

	out := make([]bqField, 0, len(ordered))
	for _, fd := range ordered {
		field, err := bigqueryField(fd)
		if err != nil {
			return nil, err
		}
		out = append(out, field)
	}
	return out, nil
}

func bigqueryField(fd protoreflect.FieldDescriptor) (bqField, error) {
	// A map field becomes a REPEATED RECORD of {key, value}, mirroring how
	// protobuf models maps as a repeated entry message.
	if fd.IsMap() {
		key, err := bigqueryField(fd.MapKey())
		if err != nil {
			return bqField{}, err
		}
		value, err := bigqueryField(fd.MapValue())
		if err != nil {
			return bqField{}, err
		}
		return bqField{
			Name:   string(fd.Name()),
			Type:   "RECORD",
			Mode:   "REPEATED",
			Fields: []bqField{key, value},
		}, nil
	}

	typ, fields, err := bigqueryType(fd)
	if err != nil {
		return bqField{}, err
	}

	mode := "NULLABLE"
	if fd.IsList() {
		mode = "REPEATED"
	}

	return bqField{Name: string(fd.Name()), Type: typ, Mode: mode, Fields: fields}, nil
}

// bigqueryType maps a protobuf field's kind to a BigQuery column type. Message
// fields become nested RECORDs (recursing into their fields) except the
// well-known Timestamp, which maps to TIMESTAMP. Any unmapped kind is an error
// so unsupported shapes fail loudly rather than silently producing a bad schema.
func bigqueryType(fd protoreflect.FieldDescriptor) (string, []bqField, error) {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return "BOOLEAN", nil, nil
	case protoreflect.StringKind:
		return "STRING", nil, nil
	case protoreflect.BytesKind:
		return "BYTES", nil, nil
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return "FLOAT", nil, nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "INTEGER", nil, nil
	case protoreflect.EnumKind:
		return "STRING", nil, nil
	case protoreflect.MessageKind, protoreflect.GroupKind:
		md := fd.Message()
		if md.FullName() == timestampFullName {
			return "TIMESTAMP", nil, nil
		}
		sub, err := bigquerySchemaFields(md)
		if err != nil {
			return "", nil, err
		}
		return "RECORD", sub, nil
	default:
		return "", nil, fmt.Errorf("unsupported field kind %s for field %s", fd.Kind(), fd.FullName())
	}
}

var validBigQueryID = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func validateBigQueryID(kind, id string) error {
	if len(id) == 0 || len(id) > maxBigQueryIDLen {
		return fmt.Errorf("%s ID %q must be 1-%d characters", kind, id, maxBigQueryIDLen)
	}
	if !validBigQueryID.MatchString(id) {
		return fmt.Errorf("%s ID %q must use only letters, numbers, or underscores", kind, id)
	}
	return nil
}

func validateBigQueryDatasetID(id string) error {
	return validateBigQueryID("dataset", id)
}

func validateBigQueryTableID(id string) error {
	return validateBigQueryID("table", id)
}
