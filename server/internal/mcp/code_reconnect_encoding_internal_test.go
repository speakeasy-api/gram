package mcp

import (
	"reflect"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func TestUpdateCodeReconnectReadsLegacyEncoding(t *testing.T) {
	t.Parallel()
	// Reconstruct the pre-CatalogCanceled shape, preserving all other field tags and order.
	binding := CodeReconnectBinding{CatalogID: uuid.New(), ReviewReady: true}
	typ := reflect.TypeFor[CodeReconnectBinding]()
	fields := []reflect.StructField{}
	for f := range typ.Fields() {
		if f := f; f.Name != "CatalogCanceled" {
			fields = append(fields, f)
		}
	}
	oldBinding := reflect.New(reflect.StructOf(fields)).Elem()
	for i := 0; i < oldBinding.NumField(); i++ {
		oldBinding.Field(i).Set(reflect.ValueOf(binding).FieldByName(oldBinding.Type().Field(i).Name))
	}
	oldBytes, err := msgpack.Marshal(oldBinding.Interface())
	require.NoError(t, err)
	newBytes, err := msgpack.Marshal(binding)
	require.NoError(t, err)
	require.Equal(t, oldBytes, newBytes, "false cancellation must not change cross-version CAS bytes")
	var decoded CodeReconnectBinding
	require.NoError(t, msgpack.Unmarshal(oldBytes, &decoded))
	roundtrip, err := msgpack.Marshal(decoded)
	require.NoError(t, err)
	require.Equal(t, oldBytes, roundtrip)

	current := AuthnChallengeState{ID: uuid.NewString(), CreatedAt: time.Now(), CodeReconnect: &binding}
	// The outer state is unchanged; only substitute the old nested binding's wire shape.
	stateType := reflect.TypeFor[AuthnChallengeState]()
	fields = nil
	for f := range stateType.Fields() {
		f := f
		if f.Name == "CodeReconnect" {
			f.Type = reflect.TypeFor[any]()
		}
		fields = append(fields, f)
	}
	legacy := reflect.New(reflect.StructOf(fields)).Elem()
	for i := 0; i < legacy.NumField(); i++ {
		if legacy.Type().Field(i).Name == "CodeReconnect" {
			legacy.Field(i).Set(oldBinding)
		} else {
			legacy.Field(i).Set(reflect.ValueOf(current).Field(i))
		}
	}
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	adapter := cache.NewRedisCacheAdapter(client)
	require.NoError(t, adapter.Set(t.Context(), current.CacheKey()+":", legacy.Interface(), current.TTL()))
	service := &Service{authnChallengeCache: cache.NewTypedObjectCache[AuthnChallengeState](testenv.NewLogger(t), adapter, cache.SuffixNone)}
	updated := binding
	updated.CatalogComplete = true
	require.NoError(t, service.updateCodeReconnect(t.Context(), current, updated))
	got, err := service.authnChallengeCache.Get(t.Context(), current.CacheKey())
	require.NoError(t, err)
	require.True(t, got.CodeReconnect.CatalogComplete)
}
