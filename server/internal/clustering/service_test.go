package clustering

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/articles/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/vector"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type fakeClusterReader struct {
	clusterOf map[uuid.UUID]uuid.UUID
	members   map[uuid.UUID][]uuid.UUID
}

func (reader *fakeClusterReader) ClusterOfArticle(_ context.Context, articleID uuid.UUID) (uuid.UUID, error) {
	clusterID, ok := reader.clusterOf[articleID]
	if !ok {
		return uuid.Nil, fmt.Errorf("clusters of article %s: %w", articleID, pgx.ErrNoRows)
	}
	return clusterID, nil
}

func (reader *fakeClusterReader) MembersOfCluster(_ context.Context, clusterID uuid.UUID) ([]uuid.UUID, error) {
	return reader.members[clusterID], nil
}

type fakeHydrator struct {
	articles map[uuid.UUID]models.StoredArticle
}

func (hydrator *fakeHydrator) GetArticlesByIDs(_ context.Context, ids []uuid.UUID) ([]models.StoredArticle, error) {
	hydrated := make([]models.StoredArticle, 0, len(ids))
	for index := len(ids) - 1; index >= 0; index-- {
		if article, ok := hydrator.articles[ids[index]]; ok {
			hydrated = append(hydrated, article)
		}
	}
	return hydrated, nil
}

func storedArticle(id uuid.UUID, title string, publishedAt time.Time) models.StoredArticle {
	return models.StoredArticle{
		ID:          id,
		Title:       title,
		Content:     title + " body",
		URL:         "https://example.test/" + id.String(),
		PublishedAt: &publishedAt,
		SourceID:    uuid.New(),
		SourceName:  "Reuters",
	}
}

func TestRelatedOrdersClusterMatesFirstAndDeduplicates(t *testing.T) {
	self := uuid.New()
	older := uuid.New()
	newer := uuid.New()
	semantic := uuid.New()
	outside := uuid.New()
	clusterID := uuid.New()
	recent := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	past := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	service := NewService(
		&fakeClusterReader{
			clusterOf: map[uuid.UUID]uuid.UUID{self: clusterID},
			members:   map[uuid.UUID][]uuid.UUID{clusterID: {self, older, newer}},
		},
		&fakeSearcher{neighbors: []vector.Neighbor{
			{ID: newer, Score: 0.95, PublishedAt: &recent},
			{ID: semantic, Score: 0.7, PublishedAt: &past},
			{ID: outside, Score: 0.5, PublishedAt: &past},
			{ID: self, Score: 0.99, PublishedAt: &recent},
		}},
		&fakeHydrator{articles: map[uuid.UUID]models.StoredArticle{
			self:     storedArticle(self, "Fed holds rates", past),
			older:    storedArticle(older, "Older mate", past),
			newer:    storedArticle(newer, "Newer mate", recent),
			semantic: storedArticle(semantic, "Semantic neighbour", past),
			outside:  storedArticle(outside, "Weak neighbour", past),
		}},
	)

	related, err := service.Related(t.Context(), self, DefaultPageSize)
	if err != nil {
		t.Fatalf("Related: %v", err)
	}

	want := []uuid.UUID{newer, older, semantic}
	if len(related) != len(want) {
		t.Fatalf("related = %d articles, want %d: %+v", len(related), len(want), related)
	}
	for index, id := range want {
		if related[index].ID != id {
			t.Errorf("related[%d] = %s, want %s", index, related[index].ID, id)
		}
	}
}

func TestRelatedServesClusterMatesWithoutAConfiguredSearcher(t *testing.T) {
	self := uuid.New()
	mate := uuid.New()
	clusterID := uuid.New()
	past := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	service := NewService(
		&fakeClusterReader{
			clusterOf: map[uuid.UUID]uuid.UUID{self: clusterID},
			members:   map[uuid.UUID][]uuid.UUID{clusterID: {self, mate}},
		},
		nil,
		&fakeHydrator{articles: map[uuid.UUID]models.StoredArticle{
			self: storedArticle(self, "Fed holds rates", past),
			mate: storedArticle(mate, "Rate decision", past),
		}},
	)

	related, err := service.Related(t.Context(), self, DefaultPageSize)
	if err != nil {
		t.Fatalf("Related: %v", err)
	}
	if len(related) != 1 || related[0].ID != mate {
		t.Fatalf("related = %+v, want only the cluster mate", related)
	}
}

func TestRelatedHonoursTheLimitBeforeSemanticFill(t *testing.T) {
	self := uuid.New()
	older := uuid.New()
	newer := uuid.New()
	semantic := uuid.New()
	clusterID := uuid.New()
	recent := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	past := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	service := NewService(
		&fakeClusterReader{
			clusterOf: map[uuid.UUID]uuid.UUID{self: clusterID},
			members:   map[uuid.UUID][]uuid.UUID{clusterID: {self, older, newer}},
		},
		&fakeSearcher{neighbors: []vector.Neighbor{
			{ID: semantic, Score: 0.9, PublishedAt: &past},
		}},
		&fakeHydrator{articles: map[uuid.UUID]models.StoredArticle{
			self:     storedArticle(self, "Fed holds rates", past),
			older:    storedArticle(older, "Older mate", past),
			newer:    storedArticle(newer, "Newer mate", recent),
			semantic: storedArticle(semantic, "Semantic neighbour", past),
		}},
	)

	related, err := service.Related(t.Context(), self, 1)
	if err != nil {
		t.Fatalf("Related: %v", err)
	}
	if len(related) != 1 || related[0].ID != newer {
		t.Fatalf("related = %+v, want the single most recent cluster mate", related)
	}
}

func TestRelatedRejectsUnknownArticles(t *testing.T) {
	service := NewService(&fakeClusterReader{}, &fakeSearcher{}, &fakeHydrator{})

	if _, err := service.Related(t.Context(), uuid.New(), DefaultPageSize); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRelatedReportsSearcherFailureAsUnavailable(t *testing.T) {
	self := uuid.New()
	past := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	service := NewService(
		&fakeClusterReader{},
		&fakeSearcher{err: errors.New("index unreachable")},
		&fakeHydrator{articles: map[uuid.UUID]models.StoredArticle{
			self: storedArticle(self, "Fed holds rates", past),
		}},
	)

	if _, err := service.Related(t.Context(), self, DefaultPageSize); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}
