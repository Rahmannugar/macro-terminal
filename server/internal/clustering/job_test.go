package clustering

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/clustering/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/vector"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type failureRecord struct {
	id          uuid.UUID
	maxAttempts int32
	cause       string
}

type createdCluster struct {
	id    uuid.UUID
	title string
}

type linkRecord struct {
	articleID uuid.UUID
	clusterID uuid.UUID
}

type fakeRepository struct {
	claimed     []models.ClaimedJob
	claimCalls  int
	articles    map[uuid.UUID]models.Article
	articleErr  error
	clusters    map[uuid.UUID]uuid.UUID
	memberships map[uuid.UUID]uuid.UUID
	titles      map[uuid.UUID]string
	created     []createdCluster
	links       []linkRecord
	touched     []uuid.UUID
	completed   []uuid.UUID
	failures    []failureRecord
	permanents  []failureRecord
}

func (repository *fakeRepository) EnqueueMissingClusterJobs(context.Context, int32) (int64, error) {
	return 0, nil
}

func (repository *fakeRepository) ReclaimStaleClusterJobs(context.Context) (int64, error) {
	return 0, nil
}

func (repository *fakeRepository) ClaimBatch(context.Context, int32) ([]models.ClaimedJob, error) {
	repository.claimCalls++
	return repository.claimed, nil
}

func (repository *fakeRepository) Complete(_ context.Context, id uuid.UUID) error {
	repository.completed = append(repository.completed, id)
	return nil
}

func (repository *fakeRepository) Fail(_ context.Context, id uuid.UUID, maxAttempts int32, cause string) error {
	repository.failures = append(repository.failures, failureRecord{id: id, maxAttempts: maxAttempts, cause: cause})
	return nil
}

func (repository *fakeRepository) FailPermanently(_ context.Context, id uuid.UUID, cause string) error {
	repository.permanents = append(repository.permanents, failureRecord{id: id, cause: cause})
	return nil
}

func (repository *fakeRepository) Article(_ context.Context, id uuid.UUID) (models.Article, error) {
	if repository.articleErr != nil {
		return models.Article{}, repository.articleErr
	}
	article, ok := repository.articles[id]
	if !ok {
		return models.Article{}, fmt.Errorf("article %s: %w", id, pgx.ErrNoRows)
	}
	return article, nil
}

func (repository *fakeRepository) ClusterOfArticle(_ context.Context, articleID uuid.UUID) (uuid.UUID, error) {
	clusterID, ok := repository.clusters[articleID]
	if !ok {
		return uuid.Nil, fmt.Errorf("clusters of article %s: %w", articleID, pgx.ErrNoRows)
	}
	return clusterID, nil
}

func (repository *fakeRepository) ClusterIDsForArticles(context.Context, []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	return repository.memberships, nil
}

func (repository *fakeRepository) CreateStoryCluster(_ context.Context, id uuid.UUID, title string) error {
	repository.created = append(repository.created, createdCluster{id: id, title: title})
	return nil
}

func (repository *fakeRepository) LinkArticleToCluster(_ context.Context, articleID, clusterID uuid.UUID) (int64, error) {
	repository.links = append(repository.links, linkRecord{articleID: articleID, clusterID: clusterID})
	return 1, nil
}

func (repository *fakeRepository) TouchStoryCluster(_ context.Context, clusterID uuid.UUID) error {
	repository.touched = append(repository.touched, clusterID)
	return nil
}

func (repository *fakeRepository) ArticleTitles(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	titles := make(map[uuid.UUID]string, len(ids))
	for _, id := range ids {
		if title, ok := repository.titles[id]; ok {
			titles[id] = title
		}
	}
	return titles, nil
}

type fakeSearcher struct {
	neighbors []vector.Neighbor
	err       error
	calls     int
}

func (searcher *fakeSearcher) FindSimilarArticles(context.Context, vector.Article, int) ([]vector.Neighbor, error) {
	searcher.calls++
	return searcher.neighbors, searcher.err
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func newTestJob(repository *fakeRepository, searcher *fakeSearcher) *Job {
	return NewJob(repository, searcher, true, testLogger())
}

func singleWork(repository *fakeRepository, articleID uuid.UUID) {
	repository.claimed = []models.ClaimedJob{{ID: uuid.New(), ArticleID: articleID, Attempts: 1}}
}

func linkedTo(repository *fakeRepository, clusterID uuid.UUID) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(repository.links))
	for _, link := range repository.links {
		if link.clusterID == clusterID {
			ids = append(ids, link.articleID)
		}
	}
	return ids
}

func containsID(ids []uuid.UUID, want uuid.UUID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestCycleCreatesClusterTitledAfterTheMostRecentNeighbour(t *testing.T) {
	selfID := uuid.New()
	older := uuid.New()
	newest := uuid.New()
	anchor := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	olderAt := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	newestAt := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		articles: map[uuid.UUID]models.Article{
			selfID: {ID: selfID, Title: "Fed holds rates", PublishedAt: &anchor},
		},
		titles: map[uuid.UUID]string{older: "Older headline", newest: "Newest headline"},
	}
	singleWork(repository, selfID)
	searcher := &fakeSearcher{neighbors: []vector.Neighbor{
		{ID: selfID, Score: 1.0, PublishedAt: &anchor},
		{ID: older, Score: 0.9, PublishedAt: &olderAt},
		{ID: newest, Score: 0.8, PublishedAt: &newestAt},
	}}

	if err := newTestJob(repository, searcher).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if len(repository.created) != 1 {
		t.Fatalf("created = %v, want one cluster", repository.created)
	}
	if repository.created[0].title != "Newest headline" {
		t.Errorf("title = %q, want the most recent member headline", repository.created[0].title)
	}
	linked := linkedTo(repository, repository.created[0].id)
	for _, id := range []uuid.UUID{selfID, older, newest} {
		if !containsID(linked, id) {
			t.Errorf("links = %v, want article %s linked", linked, id)
		}
	}
	if len(repository.touched) != 1 || repository.touched[0] != repository.created[0].id {
		t.Errorf("touched = %v, want the created cluster", repository.touched)
	}
	if len(repository.completed) != 1 {
		t.Errorf("completed = %v, want the claimed work row", repository.completed)
	}
}

func TestCycleJoinsTheClosestNeighboursExistingCluster(t *testing.T) {
	selfID := uuid.New()
	clustered := uuid.New()
	unclustered := uuid.New()
	clusterID := uuid.New()
	anchor := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		articles: map[uuid.UUID]models.Article{
			selfID: {ID: selfID, Title: "Fed holds rates", PublishedAt: &anchor},
		},
		clusters:    map[uuid.UUID]uuid.UUID{},
		memberships: map[uuid.UUID]uuid.UUID{clustered: clusterID},
	}
	singleWork(repository, selfID)
	searcher := &fakeSearcher{neighbors: []vector.Neighbor{
		{ID: clustered, Score: 0.9, PublishedAt: &anchor},
		{ID: unclustered, Score: 0.8, PublishedAt: &anchor},
	}}

	if err := newTestJob(repository, searcher).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if len(repository.created) != 0 {
		t.Fatalf("created = %v, want no new cluster", repository.created)
	}
	linked := linkedTo(repository, clusterID)
	if !containsID(linked, selfID) || !containsID(linked, unclustered) {
		t.Errorf("links = %v, want the article and its unclustered neighbour", linked)
	}
	if containsID(linked, clustered) {
		t.Errorf("links = %v, want no duplicate link for the existing member", linked)
	}
	if len(repository.completed) != 1 {
		t.Errorf("completed = %v, want the claimed work row", repository.completed)
	}
}

func TestCycleResolvesConflictingClustersToTheClosestNeighbour(t *testing.T) {
	selfID := uuid.New()
	closest := uuid.New()
	other := uuid.New()
	clusterA := uuid.New()
	clusterB := uuid.New()
	anchor := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		articles: map[uuid.UUID]models.Article{
			selfID: {ID: selfID, Title: "Fed holds rates", PublishedAt: &anchor},
		},
		memberships: map[uuid.UUID]uuid.UUID{closest: clusterA, other: clusterB},
	}
	singleWork(repository, selfID)
	searcher := &fakeSearcher{neighbors: []vector.Neighbor{
		{ID: closest, Score: 0.9, PublishedAt: &anchor},
		{ID: other, Score: 0.8, PublishedAt: &anchor},
	}}

	if err := newTestJob(repository, searcher).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if len(repository.created) != 0 {
		t.Fatalf("created = %v, want no new cluster", repository.created)
	}
	if linked := linkedTo(repository, clusterA); len(linked) != 1 || linked[0] != selfID {
		t.Errorf("cluster A links = %v, want only the article joining the closest cluster", linked)
	}
	if linked := linkedTo(repository, clusterB); len(linked) != 0 {
		t.Errorf("cluster B links = %v, want none (conflicts join without merging)", linked)
	}
}

func TestCycleIgnoresNeighboursOutsideTheWindowOrBelowThreshold(t *testing.T) {
	selfID := uuid.New()
	anchor := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	tooOld := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		articles: map[uuid.UUID]models.Article{
			selfID: {ID: selfID, Title: "Fed holds rates", PublishedAt: &anchor},
		},
	}
	singleWork(repository, selfID)
	searcher := &fakeSearcher{neighbors: []vector.Neighbor{
		{ID: selfID, Score: 1.0, PublishedAt: &anchor},
		{ID: uuid.New(), Score: 0.5, PublishedAt: &anchor},
		{ID: uuid.New(), Score: 0.9, PublishedAt: &tooOld},
		{ID: uuid.New(), Score: 0.9},
	}}

	if err := newTestJob(repository, searcher).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if len(repository.created) != 0 || len(repository.links) != 0 {
		t.Errorf("created = %v links = %v, want none", repository.created, repository.links)
	}
	if len(repository.completed) != 1 {
		t.Errorf("completed = %v, want the work row completed as unclustered", repository.completed)
	}
}

func TestCycleSkipsWorkThatNeedsNoCluster(t *testing.T) {
	selfID := uuid.New()
	clusterID := uuid.New()
	anchor := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for name, repository := range map[string]*fakeRepository{
		"already clustered": {
			articles: map[uuid.UUID]models.Article{selfID: {ID: selfID, PublishedAt: &anchor}},
			clusters: map[uuid.UUID]uuid.UUID{selfID: clusterID},
		},
		"missing publish date": {
			articles: map[uuid.UUID]models.Article{selfID: {ID: selfID}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			singleWork(repository, selfID)
			searcher := &fakeSearcher{neighbors: []vector.Neighbor{{ID: uuid.New(), Score: 0.9}}}

			if err := newTestJob(repository, searcher).cycle(t.Context()); err != nil {
				t.Fatalf("cycle: %v", err)
			}
			if searcher.calls != 0 {
				t.Errorf("searcher calls = %d, want 0", searcher.calls)
			}
			if len(repository.completed) != 1 || len(repository.links) != 0 {
				t.Errorf("completed = %v links = %v, want one completion and no links",
					repository.completed, repository.links)
			}
		})
	}
}

func TestCycleFailsMissingArticlePermanentlyWithoutSearching(t *testing.T) {
	selfID := uuid.New()
	repository := &fakeRepository{
		articleErr: fmt.Errorf("article %s: %w", selfID, pgx.ErrNoRows),
	}
	singleWork(repository, selfID)
	searcher := &fakeSearcher{neighbors: []vector.Neighbor{{ID: uuid.New(), Score: 0.9}}}

	if err := newTestJob(repository, searcher).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if searcher.calls != 0 {
		t.Errorf("searcher calls = %d, want 0", searcher.calls)
	}
	if len(repository.permanents) != 1 || repository.permanents[0].id != repository.claimed[0].ID {
		t.Errorf("permanents = %v, want the claimed row failed permanently", repository.permanents)
	}
	if len(repository.failures) != 0 || len(repository.completed) != 0 {
		t.Errorf("failures = %v completed = %v, want none", repository.failures, repository.completed)
	}
}

func TestCycleSendsSearcherFailureBackWithBackoff(t *testing.T) {
	selfID := uuid.New()
	anchor := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		articles: map[uuid.UUID]models.Article{selfID: {ID: selfID, PublishedAt: &anchor}},
	}
	singleWork(repository, selfID)
	searcher := &fakeSearcher{err: errors.New("searcher unavailable")}

	if err := newTestJob(repository, searcher).cycle(t.Context()); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	if len(repository.failures) != 1 {
		t.Fatalf("failures = %v, want one backoff failure", repository.failures)
	}
	failure := repository.failures[0]
	if failure.id != repository.claimed[0].ID || failure.maxAttempts != maxAttempts ||
		failure.cause != "searcher unavailable" {
		t.Errorf("failure = %+v, want the claimed row with the attempt budget and cause", failure)
	}
	if len(repository.completed) != 0 {
		t.Errorf("completed = %v, want none", repository.completed)
	}
}

func TestRunDisabledNeverClaimsWork(t *testing.T) {
	repository := &fakeRepository{claimed: []models.ClaimedJob{{ID: uuid.New(), ArticleID: uuid.New()}}}
	var logs bytes.Buffer
	job := NewJob(
		repository,
		&fakeSearcher{},
		false,
		slog.New(slog.NewTextHandler(&logs, nil)),
	)

	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() { result <- job.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
	if repository.claimCalls != 0 {
		t.Errorf("claim calls = %d, want 0", repository.claimCalls)
	}
	if !bytes.Contains(logs.Bytes(), []byte("missing_config")) {
		t.Errorf("logs = %s, want the missing_config skip", logs.String())
	}
}
