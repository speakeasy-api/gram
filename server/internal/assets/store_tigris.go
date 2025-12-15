package assets

// TigrisStore is a branded type around a BlobStore that is useful for
// distinguishing from other blob stores during dependency injection.
type TigrisStore struct {
	BlobStore
}

func NewTigrisStore(blobStore BlobStore) *TigrisStore {
	return &TigrisStore{
		BlobStore: blobStore,
	}
}
