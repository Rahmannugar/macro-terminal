-- Expired articles are deleted oldest-first in bounded batches so one pass
-- never holds a long transaction.

-- name: ListExpiredArticleIDs :many
SELECT id
FROM articles
WHERE published_at < $1
ORDER BY published_at
LIMIT $2;

-- name: DeleteWebhookDeliveriesForArticleNotifications :exec
DELETE FROM outbox
WHERE type = 'webhook_delivery'
  AND (payload->>'notification_id')::uuid IN (
        SELECT id FROM notifications
        WHERE subject_type = 'article'
          AND subject_id = ANY(sqlc.arg(ids)::uuid[])
      );

-- name: DeleteArticleNotifications :exec
DELETE FROM notifications
WHERE subject_type = 'article'
  AND subject_id = ANY(sqlc.arg(ids)::uuid[]);

-- name: DeleteArticleOutboxRows :exec
DELETE FROM outbox
WHERE (type IN ('article_content', 'article_index', 'article_enrichment', 'article_cluster')
       AND (payload->>'article_id')::uuid = ANY(sqlc.arg(ids)::uuid[]))
   OR (type = 'asset_notification'
       AND payload->>'subject_type' = 'article'
       AND (payload->>'subject_id')::uuid = ANY(sqlc.arg(ids)::uuid[]));

-- name: DeleteUnmappedArticlesByIDs :exec
DELETE FROM unmapped_articles
WHERE article_id = ANY(sqlc.arg(ids)::uuid[]);

-- name: DeleteArticleEntitiesByIDs :exec
DELETE FROM article_entities
WHERE article_id = ANY(sqlc.arg(ids)::uuid[]);

-- name: DeleteArticleEnrichmentsByIDs :exec
DELETE FROM article_enrichments
WHERE article_id = ANY(sqlc.arg(ids)::uuid[]);

-- name: DeleteArticleStoryClusterMembershipsByIDs :exec
DELETE FROM article_story_clusters
WHERE article_id = ANY(sqlc.arg(ids)::uuid[]);

-- name: DeleteEmptyStoryClusters :exec
DELETE FROM story_clusters
WHERE NOT EXISTS (
    SELECT 1 FROM article_story_clusters membership
    WHERE membership.story_cluster_id = story_clusters.id
);

-- name: DeleteArticlesByIDs :exec
DELETE FROM articles
WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- Completed rows are audit trail only; failed rows keep a longer replay window.

-- name: PurgeDoneOutboxRows :execrows
DELETE FROM outbox
WHERE status = 'done'
  AND updated_at < $1;

-- name: PurgeFailedOutboxRows :execrows
DELETE FROM outbox
WHERE status = 'failed'
  AND updated_at < $1;

-- name: PurgeReadNotifications :execrows
DELETE FROM notifications
WHERE read_at IS NOT NULL
  AND read_at < $1;
