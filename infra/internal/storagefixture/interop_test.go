package storagefixture

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/parquet-go/parquet-go"
	openpb "github.com/speakeasy-api/gram/infra/internal/storagefixture/pb/fixture/open"
	v1 "github.com/speakeasy-api/gram/infra/internal/storagefixture/pb/fixture/v1"
	v2 "github.com/speakeasy-api/gram/infra/internal/storagefixture/pb/fixture/v2"
	"github.com/speakeasy-api/gram/infra/pkg/storage"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func writeFixture(t *testing.T, path string, def storage.Definition, messages ...proto.Message) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close()
	w := parquet.NewWriter(f, def.Schema, parquet.MaxRowsPerRowGroup(2))
	for i, message := range messages {
		data, err := proto.Marshal(message)
		require.NoError(t, err)
		row, err := def.Decode(data, storage.Metadata{MessageID: fmt.Sprint(i), ReceivedMicros: 1791388800000000})
		require.NoError(t, err)
		_, err = w.WriteRows([]parquet.Row{row})
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
}

func TestGeneratedEncoding_DuckDBInteroperability(t *testing.T) {
	t.Parallel()
	require.Equal(t, "lake", FixtureV1Archive().Bucket)
	require.Equal(t, "fixture-archive", FixtureV2Archive().Bucket)
	root := t.TempDir()
	populated := v1.Event_builder{
		Id: new("populated"), Signed: new(int64(math.MinInt64)), Unsigned: new(uint64(math.MaxUint64)), Unsigned32: new(uint32(math.MaxUint32)),
		Flag: new(false), Data: []byte{0, 255}, Fraction: new(math.Inf(1)), SmallFraction: new(float32(-1.5)), Kind: new(v1.Event_Kind(987)),
		Child:    v1.Event_Child_builder{Value: new(""), Numbers: []int32{1, -2}}.Build(),
		Children: []*v1.Event_Child{v1.Event_Child_builder{Numbers: []int32{3, 4}}.Build(), {}, v1.Event_Child_builder{Value: new("last"), Numbers: []int32{5}}.Build()},
		Tags:     []string{"", "abc"}, Attributes: map[string]*v1.Event_Child{"z": {}, "a": v1.Event_Child_builder{Value: new("first"), Numbers: []int32{6, 7}}.Build()},
		Flags: map[bool]string{true: "yes", false: "no"}, Empty: &v1.Event_Empty{}, Empties: []*v1.Event_Empty{{}, {}},
		Number: new(int32(0)), Zigzag32: new(int32(math.MinInt32)), Zigzag64: new(int64(math.MinInt64)),
		SignedFixed32: new(int32(math.MinInt32)), SignedFixed64: new(int64(math.MinInt64)), Fixed32: new(uint32(math.MaxUint32)), Fixed64: new(uint64(math.MaxUint64)),
	}.Build()
	writeFixture(t, filepath.Join(root, "daily/part__year=2026/part__month=10/part__day=07/old.parquet"), FixtureV1Archive(),
		&v1.Event{}, populated, v1.Event_builder{Text: new("")}.Build(), v1.Event_builder{ChosenData: []byte{}}.Build(), v1.Event_builder{ChosenChild: &v1.Event_Child{}}.Build())
	writeFixture(t, filepath.Join(root, "daily/part__year=2026/part__month=10/part__day=08/new.parquet"), FixtureV2Archive(),
		v2.Event_builder{Id: new("evolved"), NewField: new(false), NewChoice: new(false), Child: v2.Event_Child_builder{NewValue: new("nested")}.Build(), Empty: v2.Event_Empty_builder{NewValue: new("was-empty")}.Build(), Empties: []*v2.Event_Empty{v2.Event_Empty_builder{NewValue: new("list")}.Build()}}.Build())
	writeFixture(t, filepath.Join(root, "hourly/part__year=2026/part__month=10/part__day=07/part__hour=14/file.parquet"), FixtureV1Archive(), populated)
	writeFixture(t, filepath.Join(root, "external/region=eu-west1/account=001/file.parquet"), FixtureV1Archive(), populated)
	writeFixture(t, filepath.Join(root, "open/file.parquet"), FixtureOpenArchive(), &openpb.Event{}, &openpb.Event{OptionalValue: new(uint64(0)), Choice: &openpb.Event_Text{Text: ""}, Child: &openpb.Event_Child{}})
	cmd := exec.CommandContext(t.Context(), "uv", "run", "--package", "gram-infra", "--no-sync", "python", "interop.py", root)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}
