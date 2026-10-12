package vector

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/ai"
	vectordb "github.com/Rahmannugar/macro-terminal/server/internal/vector/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// maxEmbedChars keeps title+content well inside text-embedding-004's
	// 2048-token window; longer bodies are truncated rather than rejected.
	maxEmbedChars = 6000
)

type pgStore struct {
	pool     *pgxpool.Pool
	queries  *vectordb.Queries
	embedder ai.Embedder
}

// NewClient builds a Postgres-backed vector client: Gemini embeds text,
// cosine similarity runs in-process over stored embeddings.
func NewClient(pool *pgxpool.Pool, embedder ai.Embedder) Client {
	return &pgStore{pool: pool, queries: vectordb.New(pool), embedder: embedder}
}

// NewIndexer returns the indexing half of the client.
func NewIndexer(pool *pgxpool.Pool, embedder ai.Embedder) Indexer {
	return NewClient(pool, embedder)
}

// NewSearcher returns the search half of the client.
func NewSearcher(pool *pgxpool.Pool, embedder ai.Embedder) Searcher {
	return NewClient(pool, embedder)
}

func (store *pgStore) StoreArticle(ctx context.Context, article Article) error {
	vector, err := store.embedder.Embed(ctx, embedInput(article))
	if err != nil {
		return fmt.Errorf("embed article: %w", err)
	}
	err = store.queries.UpsertEmbedding(ctx, vectordb.UpsertEmbeddingParams{
		ArticleID:   article.ID,
		Embedding:   vector,
		PublishedAt: pgtype.Timestamptz{Time: article.PublishedAt, Valid: !article.PublishedAt.IsZero()},
	})
	if err != nil {
		return fmt.Errorf("store article embedding: %w", err)
	}
	return nil
}

func (store *pgStore) SearchArticles(ctx context.Context, text string, limit int) ([]uuid.UUID, error) {
	if limit < 1 {
		return nil, nil
	}
	query, err := store.embedder.Embed(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("embed search query: %w", err)
	}
	rows, err := store.queries.AllEmbeddings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load embeddings: %w", err)
	}
	ranked := rankByCosine(query, rows, limit, uuid.Nil)
	ids := make([]uuid.UUID, 0, len(ranked))
	for _, hit := range ranked {
		ids = append(ids, hit.id)
	}
	return ids, nil
}

func (store *pgStore) FindSimilarArticles(ctx context.Context, article Article, limit int) ([]Neighbor, error) {
	if limit < 1 {
		return nil, nil
	}
	query, err := store.embedder.Embed(ctx, embedInput(article))
	if err != nil {
		return nil, fmt.Errorf("embed article: %w", err)
	}
	rows, err := store.queries.AllEmbeddings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load embeddings: %w", err)
	}
	ranked := rankByCosine(query, rows, limit, article.ID)
	neighbors := make([]Neighbor, 0, len(ranked))
	for _, hit := range ranked {
		neighbors = append(neighbors, Neighbor{
			ID:          hit.id,
			PublishedAt: hit.publishedAt,
			Score:       hit.score,
		})
	}
	return neighbors, nil
}

type cosineHit struct {
	id          uuid.UUID
	publishedAt *time.Time
	score       float32
}

func rankByCosine(query []float32, rows []vectordb.AllEmbeddingsRow, limit int, exclude uuid.UUID) []cosineHit {
	hits := make([]cosineHit, 0, len(rows))
	for _, row := range rows {
		if row.ArticleID == exclude {
			continue
		}
		score := cosineSimilarity(query, row.Embedding)
		var publishedAt *time.Time
		if row.PublishedAt.Valid {
			t := row.PublishedAt.Time
			publishedAt = &t
		}
		hits = append(hits, cosineHit{id: row.ArticleID, publishedAt: publishedAt, score: score})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score == hits[j].score {
			return hits[i].id.String() < hits[j].id.String()
		}
		return hits[i].score > hits[j].score
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, normA, normB float64
	for i := 0; i < n; i++ {
		x := float64(a[i])
		y := float64(b[i])
		dot += x * y
		normA += x * x
		normB += y * y
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}

func embedInput(article Article) string {
	if article.Content == "" {
		return truncateRunes(article.Title, maxEmbedChars)
	}
	body := article.Title + "\n\n" + article.Content
	return truncateRunes(body, maxEmbedChars)
}

func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}
