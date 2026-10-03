package clustering

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/clustering/models"
	"github.com/Rahmannugar/macro-terminal/server/internal/vector"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	cycleInterval  = 30 * time.Second
	enqueueLimit   = 16
	batchSize      = 8
	maxAttempts    = 8
	maxCauseLength = 500
	neighborLimit  = 50
	minSimilarity  = 0.6
	monthsInWindow = 6
)

type Repository interface {
	EnqueueMissingClusterJobs(ctx context.Context, limit int32) (int64, error)
	ReclaimStaleClusterJobs(ctx context.Context) (int64, error)
	ClaimBatch(ctx context.Context, limit int32) ([]models.ClaimedJob, error)
	Complete(ctx context.Context, id uuid.UUID) error
	Fail(ctx context.Context, id uuid.UUID, maxAttempts int32, cause string) error
	FailPermanently(ctx context.Context, id uuid.UUID, cause string) error
	Article(ctx context.Context, id uuid.UUID) (models.Article, error)
	ClusterOfArticle(ctx context.Context, articleID uuid.UUID) (uuid.UUID, error)
	ClusterIDsForArticles(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]uuid.UUID, error)
	CreateStoryCluster(ctx context.Context, id uuid.UUID, title string) error
	LinkArticleToCluster(ctx context.Context, articleID, clusterID uuid.UUID) (int64, error)
	TouchStoryCluster(ctx context.Context, clusterID uuid.UUID) error
	ArticleTitles(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error)
}

type Job struct {
	repository Repository
	searcher   VectorSearcher
	enabled    bool
	logger     *slog.Logger
	wake       chan struct{}
}

func NewJob(
	repository Repository,
	searcher VectorSearcher,
	enabled bool,
	logger *slog.Logger,
) *Job {
	return &Job{
		repository: repository,
		searcher:   searcher,
		enabled:    enabled,
		logger:     logger,
		wake:       make(chan struct{}, 1),
	}
}

func (job *Job) Wake() {
	select {
	case job.wake <- struct{}{}:
	default:
	}
}

func (job *Job) Run(ctx context.Context) error {
	if !job.enabled {
		job.logger.InfoContext(ctx,
			"Story clustering disabled until the Ahnlich address is configured",
			"event", "clustering.skipped",
			"reason", "missing_config",
		)
		<-ctx.Done()
		return ctx.Err()
	}
	ticker := time.NewTicker(cycleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		case <-job.wake:
		}
		if err := job.cycle(ctx); err != nil && !errors.Is(err, context.Canceled) {
			job.logger.ErrorContext(ctx,
				"Story clustering cycle failed",
				"event", "clustering.cycle_failed",
				"error", err,
			)
		}
	}
}

type cycleStats struct {
	reclaimed       int64
	enqueued        int64
	claimed         int
	clustered       int
	unclustered     int
	skipped         int
	failedBackoff   int
	failedPermanent int
}

func (job *Job) cycle(ctx context.Context) error {
	stats := cycleStats{}

	reclaimed, err := job.repository.ReclaimStaleClusterJobs(ctx)
	if err != nil {
		return err
	}
	stats.reclaimed = reclaimed

	enqueued, err := job.repository.EnqueueMissingClusterJobs(ctx, enqueueLimit)
	if err != nil {
		return err
	}
	stats.enqueued = enqueued
	if enqueued > 0 {
		job.logger.InfoContext(ctx,
			"Queued articles awaiting clustering",
			"event", "clustering.backfilled",
			"enqueued", enqueued,
		)
	}

	var processErr error
	for {
		claimed, err := job.repository.ClaimBatch(ctx, batchSize)
		if err != nil {
			return errors.Join(processErr, err)
		}
		stats.claimed = len(claimed)
		if len(claimed) == 0 {
			if stats.reclaimed > 0 {
				job.logCycle(ctx, stats)
			}
			return processErr
		}

		for _, work := range claimed {
			if err := job.process(ctx, work, &stats); err != nil {
				job.logger.ErrorContext(ctx,
					"Story clustering job handling failed",
					"event", "clustering.job_error",
					"outbox_id", work.ID,
					"error", err,
				)
				if ctx.Err() != nil {
					return ctx.Err()
				}
				processErr = err
			}
		}
		job.logCycle(ctx, stats)
		if len(claimed) < batchSize {
			return processErr
		}
		stats = cycleStats{}
	}
}

func (job *Job) process(ctx context.Context, work models.ClaimedJob, stats *cycleStats) error {
	article, err := job.repository.Article(ctx, work.ArticleID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			stats.failedPermanent++
			job.logger.WarnContext(ctx,
				"Clustering payload references a missing article",
				"event", "clustering.failed",
				"outbox_id", work.ID,
				"article_id", work.ArticleID,
				"reason", "article_missing",
			)
			return job.repository.FailPermanently(ctx, work.ID, "article not found")
		}
		stats.failedBackoff++
		return errors.Join(err, job.repository.Fail(ctx, work.ID, maxAttempts, truncate(err.Error(), maxCauseLength)))
	}

	if article.PublishedAt == nil {
		if err := job.repository.Complete(ctx, work.ID); err != nil {
			return err
		}
		stats.skipped++
		job.logger.InfoContext(ctx,
			"Article without a publish date cannot join a story window",
			"event", "clustering.article_skipped",
			"outbox_id", work.ID,
			"article_id", article.ID,
			"reason", "missing_publish_date",
		)
		return nil
	}

	alreadyClustered, err := job.repository.ClusterOfArticle(ctx, article.ID)
	switch {
	case err == nil:
		if err := job.repository.Complete(ctx, work.ID); err != nil {
			return err
		}
		stats.skipped++
		job.logger.InfoContext(ctx,
			"Article already belongs to a story cluster",
			"event", "clustering.article_skipped",
			"outbox_id", work.ID,
			"article_id", article.ID,
			"story_cluster_id", alreadyClustered,
			"reason", "already_clustered",
		)
		return nil
	case errors.Is(err, pgx.ErrNoRows):
	default:
		stats.failedBackoff++
		return errors.Join(err, job.repository.Fail(ctx, work.ID, maxAttempts, truncate(err.Error(), maxCauseLength)))
	}

	started := time.Now()
	neighbors, err := job.searcher.FindSimilarArticles(ctx, vector.Article{
		ID:          article.ID,
		PublishedAt: *article.PublishedAt,
		Title:       article.Title,
		Content:     article.Content,
	}, neighborLimit)
	if err != nil {
		return job.fail(ctx, work, stats, err)
	}

	qualifying := make([]vector.Neighbor, 0, len(neighbors))
	for _, neighbor := range neighbors {
		if neighbor.ID == article.ID || neighbor.Score < minSimilarity || neighbor.PublishedAt == nil {
			continue
		}
		if !withinWindow(*article.PublishedAt, *neighbor.PublishedAt) {
			continue
		}
		qualifying = append(qualifying, neighbor)
	}
	if len(qualifying) == 0 {
		if err := job.repository.Complete(ctx, work.ID); err != nil {
			return err
		}
		stats.unclustered++
		job.logger.InfoContext(ctx,
			"No qualifying story neighbours found",
			"event", "clustering.article_unclustered",
			"outbox_id", work.ID,
			"article_id", article.ID,
			"considered", len(neighbors),
		)
		return nil
	}

	neighborIDs := make([]uuid.UUID, 0, len(qualifying))
	for _, neighbor := range qualifying {
		neighborIDs = append(neighborIDs, neighbor.ID)
	}
	memberships, err := job.repository.ClusterIDsForArticles(ctx, neighborIDs)
	if err != nil {
		return job.fail(ctx, work, stats, err)
	}

	clusterID, outcome, err := job.resolveCluster(ctx, article, qualifying, memberships)
	if err != nil {
		return job.fail(ctx, work, stats, err)
	}
	if err := job.repository.Complete(ctx, work.ID); err != nil {
		return err
	}
	stats.clustered++

	job.logger.InfoContext(ctx,
		"Article assigned to a story cluster",
		"event", "clustering.article_clustered",
		"outbox_id", work.ID,
		"article_id", article.ID,
		"story_cluster_id", clusterID,
		"outcome", outcome,
		"considered", len(neighbors),
		"qualifying", len(qualifying),
		"duration_ms", time.Since(started).Milliseconds(),
	)
	return nil
}

// resolveCluster creates a cluster when no neighbour belongs to one, otherwise joins the closest neighbour's cluster.
func (job *Job) resolveCluster(
	ctx context.Context,
	article models.Article,
	qualifying []vector.Neighbor,
	memberships map[uuid.UUID]uuid.UUID,
) (uuid.UUID, string, error) {
	clusters := make(map[uuid.UUID]struct{})
	var closest uuid.UUID
	for _, neighbor := range qualifying {
		clusterID, ok := memberships[neighbor.ID]
		if !ok {
			continue
		}
		clusters[clusterID] = struct{}{}
		if closest == uuid.Nil {
			closest = neighbor.ID
		}
	}

	if closest == uuid.Nil {
		clusterID := uuid.New()
		titles, err := job.articleTitles(ctx, article, qualifying)
		if err != nil {
			return uuid.Nil, "", err
		}
		if err := job.repository.CreateStoryCluster(ctx, clusterID, mostRecentTitle(article, qualifying, titles)); err != nil {
			return uuid.Nil, "", err
		}
		if _, err := job.repository.LinkArticleToCluster(ctx, article.ID, clusterID); err != nil {
			return uuid.Nil, "", err
		}
		for _, neighbor := range qualifying {
			if _, err := job.repository.LinkArticleToCluster(ctx, neighbor.ID, clusterID); err != nil {
				return uuid.Nil, "", err
			}
		}
		if err := job.repository.TouchStoryCluster(ctx, clusterID); err != nil {
			return uuid.Nil, "", err
		}
		return clusterID, "created", nil
	}

	clusterID := memberships[closest]
	if len(clusters) > 1 {
		job.logger.WarnContext(ctx,
			"Qualifying neighbours span multiple story clusters",
			"event", "clustering.multi_cluster",
			"article_id", article.ID,
			"story_cluster_id", clusterID,
			"clusters_found", len(clusters),
		)
	}
	if _, err := job.repository.LinkArticleToCluster(ctx, article.ID, clusterID); err != nil {
		return uuid.Nil, "", err
	}
	for _, neighbor := range qualifying {
		if _, linked := memberships[neighbor.ID]; linked {
			continue
		}
		if _, err := job.repository.LinkArticleToCluster(ctx, neighbor.ID, clusterID); err != nil {
			return uuid.Nil, "", err
		}
	}
	if err := job.repository.TouchStoryCluster(ctx, clusterID); err != nil {
		return uuid.Nil, "", err
	}
	if len(clusters) > 1 {
		return clusterID, "conflict", nil
	}
	return clusterID, "joined", nil
}

func (job *Job) articleTitles(ctx context.Context, article models.Article, qualifying []vector.Neighbor) (map[uuid.UUID]string, error) {
	ids := make([]uuid.UUID, 0, len(qualifying)+1)
	ids = append(ids, article.ID)
	for _, neighbor := range qualifying {
		ids = append(ids, neighbor.ID)
	}
	titles, err := job.repository.ArticleTitles(ctx, ids)
	if err != nil {
		return nil, err
	}
	if _, ok := titles[article.ID]; !ok {
		titles[article.ID] = article.Title
	}
	return titles, nil
}

func mostRecentTitle(article models.Article, qualifying []vector.Neighbor, titles map[uuid.UUID]string) string {
	mostRecent := *article.PublishedAt
	title := article.Title
	for _, neighbor := range qualifying {
		if neighbor.PublishedAt != nil && neighbor.PublishedAt.After(mostRecent) {
			mostRecent = *neighbor.PublishedAt
			title = titles[neighbor.ID]
		}
	}
	if title == "" {
		return article.Title
	}
	return title
}

func withinWindow(anchor, other time.Time) bool {
	return !other.Before(anchor.AddDate(0, -monthsInWindow, 0)) &&
		!other.After(anchor.AddDate(0, monthsInWindow, 0))
}

func (job *Job) fail(ctx context.Context, work models.ClaimedJob, stats *cycleStats, cause error) error {
	if work.Attempts >= maxAttempts {
		stats.failedPermanent++
	} else {
		stats.failedBackoff++
	}
	job.logger.WarnContext(ctx,
		"Clustering attempt failed",
		"event", "clustering.failed",
		"outbox_id", work.ID,
		"article_id", work.ArticleID,
		"attempts", work.Attempts,
		"error", truncate(cause.Error(), maxCauseLength),
	)
	return job.repository.Fail(ctx, work.ID, maxAttempts, truncate(cause.Error(), maxCauseLength))
}

func (job *Job) logCycle(ctx context.Context, stats cycleStats) {
	job.logger.InfoContext(ctx,
		"Story clustering cycle finished",
		"event", "clustering.cycle",
		"reclaimed", stats.reclaimed,
		"enqueued", stats.enqueued,
		"claimed", stats.claimed,
		"clustered", stats.clustered,
		"unclustered", stats.unclustered,
		"skipped", stats.skipped,
		"failed_backoff", stats.failedBackoff,
		"failed_permanent", stats.failedPermanent,
	)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
