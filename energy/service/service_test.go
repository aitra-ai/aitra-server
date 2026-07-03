package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStartDisabledWhenNoPrometheusAddr(t *testing.T) {
	// With no Prometheus address the feature is off: no panic, no db access,
	// no metric registration. db is nil to prove it is untouched.
	require.NotPanics(t, func() {
		Start(context.Background(), nil, Options{})
	})
}

func TestOpenStorageNoneIsNil(t *testing.T) {
	require.Nil(t, openStorage(context.Background(), Options{StorageBackend: ""}))
	require.Nil(t, openStorage(context.Background(), Options{StorageBackend: "none"}))
}

func TestOpenStorageUnknownDegradesToNil(t *testing.T) {
	// An unknown backend name must not block the (opt-in) feature.
	require.Nil(t, openStorage(context.Background(), Options{StorageBackend: "does-not-exist"}))
}

func TestOpenStorageSqliteOpensAndClosesOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := openStorage(ctx, Options{StorageBackend: "sqlite", StoragePath: t.TempDir() + "/energy.db"})
	require.NotNil(t, store, "sqlite backend constructs from the registry")
	cancel() // triggers the close goroutine; must not panic or double-close badly
}
