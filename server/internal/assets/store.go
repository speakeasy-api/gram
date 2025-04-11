package assets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"cloud.google.com/go/storage"
)

type BlobStore interface {
	Exists(ctx context.Context, objectURL *url.URL) (bool, error)
	Read(ctx context.Context, objectURL *url.URL) (io.ReadCloser, error)
	Write(ctx context.Context, urlpath string, src io.Reader, contentType string) (io.WriteCloser, *url.URL, error)
}

type FSBlobStore struct {
	mut  sync.Mutex
	Root *os.Root
}

var _ BlobStore = (*FSBlobStore)(nil)

func (fbs *FSBlobStore) getPath(u *url.URL) (string, error) {
	if u.Scheme != "file" {
		return "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}

	p := strings.TrimPrefix(u.String(), "file://")
	return filepath.Join(strings.Split(p, "/")...), nil
}

func (fbs *FSBlobStore) Exists(ctx context.Context, u *url.URL) (bool, error) {
	filepath, err := fbs.getPath(u)
	if err != nil {
		return false, fmt.Errorf("generate asset path: %w", err)
	}

	fbs.mut.Lock()
	defer fbs.mut.Unlock()

	stat, err := fbs.Root.Stat(filepath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("stat file: %w", err)
	default:
		return stat.Mode().IsRegular(), nil
	}
}
func (fbs *FSBlobStore) Read(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	filepath, err := fbs.getPath(u)
	if err != nil {
		return nil, fmt.Errorf("generate asset path: %w", err)
	}

	fbs.mut.Lock()
	defer fbs.mut.Unlock()

	return fbs.Root.Open(filepath)
}

func (fbs *FSBlobStore) Write(ctx context.Context, pathname string, src io.Reader, contentType string) (io.WriteCloser, *url.URL, error) {
	fbs.mut.Lock()
	defer fbs.mut.Unlock()

	fspath := filepath.Join(strings.Split(pathname, "/")...)
	if err := fbs.mkdirAll(fspath); err != nil {
		return nil, nil, err
	}

	dst, err := fbs.Root.OpenFile(fspath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, nil, err
	}

	return dst, &url.URL{Scheme: "file", Path: fspath}, nil
}

func (fbs *FSBlobStore) mkdirAll(filename string) error {
	dir := filepath.Dir(filepath.Clean(filename))

	segments := []string{}
	limit := 10 // hard limit to prevent infinite loops
	for i := range limit {
		if dir == "" || dir == "." || dir == "/" {
			break
		}

		segments = append(segments, dir)
		next, _ := filepath.Split(dir)
		if next == dir {
			break
		}
		dir = next

		if i == limit-1 {
			return fmt.Errorf("too many segments: %s", filename)
		}
	}

	for _, seg := range slices.Backward(segments) {
		if err := fbs.Root.Mkdir(seg, 0755); err != nil && !os.IsExist(err) {
			return err
		}
	}

	return nil
}

type GCSBlobStore struct {
	client    *storage.Client
	bucket    *storage.BucketHandle
	bucketURI *url.URL
}

var _ BlobStore = (*GCSBlobStore)(nil)

func NewGCSBlobStore(ctx context.Context, bucketURI string) (*GCSBlobStore, error) {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create gcs client: %w", err)
	}

	uri, err := url.Parse(bucketURI)
	if err != nil {
		return nil, fmt.Errorf("parse bucket uri: %w", err)
	}

	bucket := client.Bucket(uri.Hostname())

	return &GCSBlobStore{client: client, bucket: bucket, bucketURI: uri}, nil
}

func (gbs *GCSBlobStore) getPath(u *url.URL) (string, error) {
	if u.Scheme != "gs" {
		return "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}

	bu := gbs.bucketURI.String()
	if !strings.HasSuffix(bu, "/") {
		bu += "/"
	}

	if !strings.HasPrefix(u.String(), bu) {
		return "", fmt.Errorf("unauthorized access")
	}

	return strings.TrimPrefix(u.Path, "/"), nil
}

func (gbs *GCSBlobStore) getBucketURI(subpath string) (*url.URL, error) {
	noPrefix := strings.TrimPrefix(subpath, gbs.bucketURI.Path)
	noSlash := strings.TrimPrefix(noPrefix, "/")
	u := gbs.bucketURI.JoinPath(noSlash)
	us := u.String()
	if us == "" || !strings.HasPrefix(us, gbs.bucketURI.String()) || us == gbs.bucketURI.String() {
		return nil, fmt.Errorf("invalid path: %s", subpath)
	}

	return u, nil
}

func (gbs *GCSBlobStore) Exists(ctx context.Context, u *url.URL) (bool, error) {
	subpath, err := gbs.getPath(u)
	if err != nil {
		return false, fmt.Errorf("generate asset path: %w", err)
	}

	obj := gbs.bucket.Object(subpath)
	_, err = obj.Attrs(ctx)
	switch {
	case errors.Is(err, storage.ErrObjectNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("get object attrs: %w", err)
	default:
		return true, nil
	}
}

func (gbs *GCSBlobStore) Read(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	subpath, err := gbs.getPath(u)
	if err != nil {
		return nil, fmt.Errorf("generate asset path: %w", err)
	}

	return gbs.bucket.Object(subpath).NewReader(ctx)
}

func (gbs *GCSBlobStore) Write(ctx context.Context, subpath string, src io.Reader, contentType string) (io.WriteCloser, *url.URL, error) {
	uri, err := gbs.getBucketURI(subpath)
	if err != nil {
		return nil, nil, fmt.Errorf("generate asset path: %w", err)
	}

	w := gbs.bucket.Object(strings.TrimPrefix(uri.Path, "/")).NewWriter(ctx)
	w.ContentType = contentType

	return w, uri, nil
}
