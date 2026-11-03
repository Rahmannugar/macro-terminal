package vector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/Rahmannugar/macro-terminal/server/internal/ai"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/circuitbreaker"
	"github.com/deven96/ahnlich/sdk/ahnlich-client-go/grpc/algorithm/algorithms"
	dbquery "github.com/deven96/ahnlich/sdk/ahnlich-client-go/grpc/db/query"
	dbserver "github.com/deven96/ahnlich/sdk/ahnlich-client-go/grpc/db/server"
	"github.com/deven96/ahnlich/sdk/ahnlich-client-go/grpc/keyval"
	"github.com/deven96/ahnlich/sdk/ahnlich-client-go/grpc/metadata"
	"github.com/deven96/ahnlich/sdk/ahnlich-client-go/grpc/predicates"
	dbsvc "github.com/deven96/ahnlich/sdk/ahnlich-client-go/grpc/services/db_service"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var ErrNotIndexed = errors.New("article has no stored vector")

const (
	articleStore   = "articles"
	articleIDKey   = "article_id"
	publishedAtKey = "published_at"
	requestWindow  = 60 * time.Second
	// Bound vector payloads to the supported search window and its lookahead.
	maxSearchNeighbors = 201
	// Bound UTF-8 bytes as well as batch cost for non-English input.
	maxEmbedChars = 6000
)

var articlePredicates = []string{articleIDKey, publishedAtKey}

type ahnlichStore struct {
	conn     *grpc.ClientConn
	client   dbsvc.DBServiceClient
	embedder ai.Embedder
}

// NewClient dials Ahnlich DB once; Gemini embeds text and the store keeps the vectors.
func NewClient(addr string, embedder ai.Embedder, circuit circuitbreaker.Controller) (Client, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(circuitInterceptor(circuit, "ahnlich:"+addr)))
	if err != nil {
		return nil, fmt.Errorf("create Ahnlich client for %q: %w", addr, err)
	}
	return &ahnlichStore{conn: conn, client: dbsvc.NewDBServiceClient(conn), embedder: embedder}, nil
}

// NewIndexer returns the indexing half of the client.
func NewIndexer(addr string, embedder ai.Embedder, circuit circuitbreaker.Controller) (Indexer, error) {
	return NewClient(addr, embedder, circuit)
}

// NewSearcher returns the search half of the client.
func NewSearcher(addr string, embedder ai.Embedder, circuit circuitbreaker.Controller) (Searcher, error) {
	return NewClient(addr, embedder, circuit)
}

func (store *ahnlichStore) StoreArticle(ctx context.Context, article Article) error {
	remaining, err := store.StoreArticles(ctx, []Article{article})
	if err != nil {
		return err
	}
	if len(remaining) > 0 && remaining[0] != nil {
		return remaining[0]
	}
	return nil
}

func (store *ahnlichStore) StoreArticles(ctx context.Context, articles []Article) ([]error, error) {
	if len(articles) == 0 {
		return nil, nil
	}

	inputs := make([]string, len(articles))
	for index, article := range articles {
		inputs[index] = embedInput(article)
	}
	errs := make([]error, len(articles))
	for start := 0; start < len(inputs); {
		end := ai.EmbeddingBatchEnd(inputs, start)
		started := time.Now()
		vectors, err := store.embedder.EmbedBatch(ctx, inputs[start:end])
		tokens := 0
		for _, input := range inputs[start:end] {
			tokens += ai.EmbeddingCost(input)
		}
		if err != nil {
			// Keep earlier chunks stored; only unfinished work is retried.
			for index := start; index < len(errs); index++ {
				errs[index] = fmt.Errorf("embed articles: %w", err)
			}
			return errs, nil
		}
		chunkCtx, cancel := context.WithTimeout(ctx, requestWindow)
		err = store.ensureStore(chunkCtx, uint32(len(vectors[0])))
		for index := start; index < end; index++ {
			if err != nil {
				errs[index] = err
			} else {
				errs[index] = store.storeVector(chunkCtx, articles[index], vectors[index-start])
			}
		}
		cancel()
		slog.InfoContext(ctx, "Embedding batch completed", "event", "embedding.batch_completed", "articles", end-start,
			"estimated_tokens", tokens, "duration_ms", time.Since(started).Milliseconds())
		start = end
	}
	return errs, nil
}

func (store *ahnlichStore) storeVector(ctx context.Context, article Article, vector []float32) error {
	condition := articleCondition(article.ID)
	existing, err := store.client.GetPred(ctx, &dbquery.GetPred{
		Store:     articleStore,
		Condition: condition,
	})
	if err != nil {
		return fmt.Errorf("look up existing article vector: %w", err)
	}
	if len(existing.Entries) > 0 {
		if _, err := store.client.DelPred(ctx, &dbquery.DelPred{
			Store:     articleStore,
			Condition: condition,
		}); err != nil {
			return fmt.Errorf("remove stale article vector: %w", err)
		}
	}

	_, err = store.client.Set(ctx, &dbquery.Set{
		Store: articleStore,
		Inputs: []*keyval.DbStoreEntry{{
			Key: &keyval.StoreKey{Key: vector},
			Value: &keyval.StoreValue{Value: map[string]*metadata.MetadataValue{
				articleIDKey:   metadataValue(article.ID.String()),
				publishedAtKey: metadataValue(formatPublishedAt(article.PublishedAt)),
			}},
		}},
	})
	if err != nil {
		return fmt.Errorf("store article vector: %w", err)
	}
	return nil
}

// ensureStore creates the store with the embedding dimension on first use; repeat calls are no-ops unless the dimension changed.
func (store *ahnlichStore) ensureStore(ctx context.Context, dimension uint32) error {
	if _, err := store.client.CreateStore(ctx, &dbquery.CreateStore{
		Store:            articleStore,
		Dimension:        dimension,
		CreatePredicates: articlePredicates,
	}); err != nil {
		return fmt.Errorf("ensure article store: %w", err)
	}
	info, err := store.client.GetStore(ctx, &dbquery.GetStore{Store: articleStore})
	if err != nil {
		return fmt.Errorf("read article store info: %w", err)
	}
	if info.Dimension != dimension {
		return fmt.Errorf("article store dimension is %d but embeddings are %d; drop the %q store to rebuild it",
			info.Dimension, dimension, articleStore)
	}
	if _, err := store.client.CreatePredIndex(ctx, &dbquery.CreatePredIndex{
		Store:      articleStore,
		Predicates: articlePredicates,
	}); err != nil {
		return fmt.Errorf("ensure article predicates: %w", err)
	}
	return nil
}

// DeleteArticles removes each article's vector by its article_id predicate. A missing
// article is a no-op so retention never fails on a vector that was already removed.
func (store *ahnlichStore) DeleteArticles(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, requestWindow)
	defer cancel()

	for _, id := range ids {
		if _, err := store.client.DelPred(ctx, &dbquery.DelPred{
			Store:     articleStore,
			Condition: articleCondition(id),
		}); err != nil {
			return fmt.Errorf("delete article vector %s: %w", id, err)
		}
	}
	return nil
}

func (store *ahnlichStore) SearchArticles(ctx context.Context, text string, limit int) ([]uuid.UUID, error) {
	vector, err := store.embedder.Embed(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("embed search query: %w", err)
	}
	entries, err := store.closest(ctx, vector, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(entries))
	for _, entry := range entries {
		id, err := uuid.Parse(storeValueString(entry.Value, articleIDKey))
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (store *ahnlichStore) FindSimilarArticles(ctx context.Context, article Article, limit int) ([]Neighbor, error) {
	ctx, span := otel.Tracer("github.com/Rahmannugar/macro-terminal/server/internal/vector").Start(ctx, "vector.article.related")
	defer span.End()
	ctx, cancel := context.WithTimeout(ctx, requestWindow)
	defer cancel()
	existing, err := store.client.GetPred(ctx, &dbquery.GetPred{Store: articleStore, Condition: articleCondition(article.ID)})
	if err != nil {
		span.SetStatus(codes.Error, "vector_lookup_failed")
		span.RecordError(err)
		return nil, fmt.Errorf("load stored article vector: %w", err)
	}
	if len(existing.Entries) == 0 || existing.Entries[0].Key == nil {
		span.SetAttributes(attribute.String("vector.outcome", "not_indexed"))
		return nil, ErrNotIndexed
	}
	span.SetAttributes(attribute.String("vector.source", "stored"))
	entries, err := store.closest(ctx, existing.Entries[0].Key.Key, limit+1)
	if err != nil {
		span.SetStatus(codes.Error, "vector_search_failed")
		span.RecordError(err)
		return nil, err
	}
	span.SetAttributes(attribute.String("vector.outcome", "success"))
	neighbors := make([]Neighbor, 0, len(entries))
	for _, entry := range entries {
		id, err := uuid.Parse(storeValueString(entry.Value, articleIDKey))
		if err != nil || entry.Similarity == nil {
			continue
		}
		if id == article.ID {
			continue
		}
		neighbors = append(neighbors, Neighbor{
			ID:          id,
			PublishedAt: parsePublishedAt(storeValueString(entry.Value, publishedAtKey)),
			Score:       entry.Similarity.Value,
		})
		if len(neighbors) >= limit {
			break
		}
	}
	return neighbors, nil
}

// closest asks the index for the nearest entries; cosine keeps scores on the higher-is-closer scale callers threshold against.
func (store *ahnlichStore) closest(ctx context.Context, vector []float32, limit int) ([]*dbserver.GetSimNEntry, error) {
	if limit < 1 {
		return nil, nil
	}
	if limit > maxSearchNeighbors {
		return nil, fmt.Errorf("vector search limit %d exceeds maximum %d", limit, maxSearchNeighbors)
	}
	ctx, cancel := context.WithTimeout(ctx, requestWindow)
	defer cancel()

	response, err := store.client.GetSimN(ctx, &dbquery.GetSimN{
		Store:       articleStore,
		SearchInput: &keyval.StoreKey{Key: vector},
		Algorithm:   algorithms.Algorithm_CosineSimilarity,
		ClosestN:    uint64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search article vectors: %w", err)
	}
	return response.Entries, nil
}

func storeValueString(value *keyval.StoreValue, key string) string {
	if value == nil {
		return ""
	}
	item, ok := value.Value[key]
	if !ok || item == nil {
		return ""
	}
	raw, ok := item.Value.(*metadata.MetadataValue_RawString)
	if !ok {
		return ""
	}
	return raw.RawString
}

func articleCondition(articleID uuid.UUID) *predicates.PredicateCondition {
	return &predicates.PredicateCondition{
		Kind: &predicates.PredicateCondition_Value{
			Value: &predicates.Predicate{
				Kind: &predicates.Predicate_Equals{
					Equals: &predicates.Equals{
						Key:   articleIDKey,
						Value: metadataValue(articleID.String()),
					},
				},
			},
		},
	}
}

func embedInput(article Article) string {
	if article.Content == "" {
		return truncateBytes(article.Title, maxEmbedChars)
	}
	body := article.Title + "\n\n" + article.Content
	return truncateBytes(body, maxEmbedChars)
}

func truncateBytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	for !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}

func formatPublishedAt(publishedAt time.Time) string {
	if publishedAt.IsZero() {
		return ""
	}
	return publishedAt.UTC().Format(time.RFC3339)
}

func parsePublishedAt(value string) *time.Time {
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	return &parsed
}

func metadataValue(value string) *metadata.MetadataValue {
	return &metadata.MetadataValue{Value: &metadata.MetadataValue_RawString{RawString: value}}
}
