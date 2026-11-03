package vector

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Rahmannugar/macro-terminal/server/internal/ai"
	dbquery "github.com/deven96/ahnlich/sdk/ahnlich-client-go/grpc/db/query"
	dbserver "github.com/deven96/ahnlich/sdk/ahnlich-client-go/grpc/db/server"
	"github.com/deven96/ahnlich/sdk/ahnlich-client-go/grpc/keyval"
	dbsvc "github.com/deven96/ahnlich/sdk/ahnlich-client-go/grpc/services/db_service"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

type testDB struct {
	dbsvc.DBServiceClient
	existing []*keyval.DbStoreEntry
	searched []float32
	stored   int
}

func (db *testDB) GetPred(context.Context, *dbquery.GetPred, ...grpc.CallOption) (*dbserver.Get, error) {
	return &dbserver.Get{Entries: db.existing}, nil
}
func (db *testDB) GetSimN(_ context.Context, request *dbquery.GetSimN, _ ...grpc.CallOption) (*dbserver.GetSimN, error) {
	db.searched = request.SearchInput.Key
	return &dbserver.GetSimN{}, nil
}
func (*testDB) CreateStore(context.Context, *dbquery.CreateStore, ...grpc.CallOption) (*dbserver.Unit, error) {
	return &dbserver.Unit{}, nil
}
func (*testDB) GetStore(context.Context, *dbquery.GetStore, ...grpc.CallOption) (*dbserver.StoreInfo, error) {
	return &dbserver.StoreInfo{Dimension: 2}, nil
}
func (*testDB) CreatePredIndex(context.Context, *dbquery.CreatePredIndex, ...grpc.CallOption) (*dbserver.CreateIndex, error) {
	return &dbserver.CreateIndex{}, nil
}
func (db *testDB) Set(context.Context, *dbquery.Set, ...grpc.CallOption) (*dbserver.Set, error) {
	db.stored++
	return &dbserver.Set{}, nil
}

func TestRelatedUsesStoredVectorWithoutAnEmbedder(t *testing.T) {
	db := &testDB{existing: []*keyval.DbStoreEntry{{Key: &keyval.StoreKey{Key: []float32{0.3, 0.7}}}}}
	store := &ahnlichStore{client: db}
	if _, err := store.FindSimilarArticles(t.Context(), Article{ID: uuid.New()}, 10); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(db.searched, []float32{0.3, 0.7}) {
		t.Fatalf("searched vector = %v, want stored vector", db.searched)
	}
	db.existing = nil
	if _, err := store.FindSimilarArticles(t.Context(), Article{ID: uuid.New()}, 10); !errors.Is(err, ErrNotIndexed) {
		t.Fatalf("missing vector error = %v", err)
	}
}

type chunkEmbedder struct{ calls int }

func (*chunkEmbedder) Embed(context.Context, string) ([]float32, error) {
	panic("unexpected query embedding")
}
func (embedder *chunkEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	embedder.calls++
	if embedder.calls > 1 {
		return nil, &ai.DeferredError{Reason: "local_budget", Delay: time.Minute}
	}
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = []float32{0.3, 0.7}
	}
	return vectors, nil
}

func TestBatchDeferralPreservesStoredChunks(t *testing.T) {
	db := &testDB{}
	embedder := &chunkEmbedder{}
	store := &ahnlichStore{client: db, embedder: embedder}
	articles := make([]Article, 39)
	for i := range articles {
		articles[i] = Article{ID: uuid.New(), Content: strings.Repeat("x", 6000)}
	}
	errs, err := store.StoreArticles(t.Context(), articles)
	if err != nil {
		t.Fatal(err)
	}
	if db.stored != 3 || len(errs) != 39 {
		t.Fatalf("stored=%d errors=%d", db.stored, len(errs))
	}
	for i, itemErr := range errs {
		if i < 3 && itemErr != nil {
			t.Fatalf("stored article failed: %v", itemErr)
		}
		if i >= 3 {
			if _, ok := ai.ProviderDeferral(itemErr); !ok {
				t.Fatalf("unfinished article %d not deferred: %v", i, itemErr)
			}
		}
	}
}

func TestEmbeddingInputBoundsUTF8Bytes(t *testing.T) {
	input := embedInput(Article{Content: strings.Repeat("€", 6000)})
	if len(input) > maxEmbedChars || !utf8.ValidString(input) {
		t.Fatalf("invalid or oversized UTF-8: %d bytes", len(input))
	}
}

func TestOversizedSimilarityLookupIsRejectedBeforeRPC(t *testing.T) {
	store := &ahnlichStore{}
	if _, err := store.closest(t.Context(), []float32{0.3}, 10000); err == nil {
		t.Fatal("oversized similarity lookup was accepted")
	}
}
