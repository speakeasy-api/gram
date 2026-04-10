package drafts_test

import (
	"context"
	"io"
	"log"
	"os"
	"testing"

	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/corpus/drafts"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{
		Postgres: true,
		Redis:    true,
	})
	if err != nil {
		log.Fatalf("launch test infra: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infra: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	svc       *drafts.Service
	conn      *pgxpool.Pool
	projectID uuid.UUID
	orgID     string
	repoDir   string
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "corpus_drafts")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	_ = tracerProvider

	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, conn, redisClient, cache.Suffix("gram-local"), billingClient)

	ctx := testenv.InitAuthContext(t, t.Context(), conn, sessionManager)

	authctx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authctx.ProjectID)

	repoDir := t.TempDir()
	gitRepo := newTestGitRepo(t, repoDir)

	svc := drafts.NewService(conn, gitRepo)

	return ctx, &testInstance{
		svc:       svc,
		conn:      conn,
		projectID: *authctx.ProjectID,
		orgID:     authctx.ActiveOrganizationID,
		repoDir:   repoDir,
	}
}

// testGitRepo implements drafts.GitRepo using go-git directly.
type testGitRepo struct {
	repo *gogit.Repository
	path string
}

func newTestGitRepo(t *testing.T, dir string) *testGitRepo {
	t.Helper()
	r, err := gogit.PlainInit(dir, true)
	require.NoError(t, err)
	return &testGitRepo{repo: r, path: dir}
}

func (r *testGitRepo) CommitFiles(files map[string][]byte, message string) (string, error) {
	storer := r.repo.Storer

	// Build tree
	var entries []object.TreeEntry
	for path, content := range files {
		obj := storer.NewEncodedObject()
		obj.SetType(plumbing.BlobObject)
		obj.SetSize(int64(len(content)))
		w, err := obj.Writer()
		if err != nil {
			return "", err
		}
		if _, err := w.Write(content); err != nil {
			return "", err
		}
		if err := w.Close(); err != nil {
			return "", err
		}
		blobHash, err := storer.SetEncodedObject(obj)
		if err != nil {
			return "", err
		}
		entries = append(entries, object.TreeEntry{
			Name: path,
			Mode: 0o100644,
			Hash: blobHash,
		})
	}

	tree := &object.Tree{Entries: entries, Hash: plumbing.ZeroHash}
	treeObj := storer.NewEncodedObject()
	if err := tree.Encode(treeObj); err != nil {
		return "", err
	}
	treeHash, err := storer.SetEncodedObject(treeObj)
	if err != nil {
		return "", err
	}

	var parents []plumbing.Hash
	if headRef, err := r.repo.Head(); err == nil {
		parents = append(parents, headRef.Hash())
	}

	commit := &object.Commit{
		Hash:         plumbing.ZeroHash,
		Author:       object.Signature{Name: "test", Email: "test@test.com"},
		Committer:    object.Signature{Name: "test", Email: "test@test.com"},
		MergeTag:     "",
		Signature:    "",
		Message:      message,
		TreeHash:     treeHash,
		ParentHashes: parents,
		Encoding:     "",
		ExtraHeaders: nil,
	}

	commitObj := storer.NewEncodedObject()
	if err := commit.Encode(commitObj); err != nil {
		return "", err
	}
	commitHash, err := storer.SetEncodedObject(commitObj)
	if err != nil {
		return "", err
	}

	ref := plumbing.NewHashReference(plumbing.Master, commitHash)
	if err := storer.SetReference(ref); err != nil {
		return "", err
	}
	headSymRef := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.Master)
	if err := storer.SetReference(headSymRef); err != nil {
		return "", err
	}

	return commitHash.String(), nil
}

func (r *testGitRepo) ReadBlob(ref string, path string) ([]byte, error) {
	hash, err := r.repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return nil, err
	}
	commit, err := r.repo.CommitObject(*hash)
	if err != nil {
		return nil, err
	}
	file, err := commit.File(path)
	if err != nil {
		return nil, err
	}
	reader, err := file.Reader()
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	return io.ReadAll(reader)
}

func (r *testGitRepo) ReadTree(ref string) ([]drafts.TreeEntry, error) {
	hash, err := r.repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return nil, err
	}
	commit, err := r.repo.CommitObject(*hash)
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	var result []drafts.TreeEntry
	for _, entry := range tree.Entries {
		if entry.Mode.IsFile() {
			result = append(result, drafts.TreeEntry{Path: entry.Name})
		}
	}
	return result, nil
}
