-- name: CreateStoryCluster :one
INSERT INTO story_clusters (id, title)
VALUES ($1, $2)
RETURNING id;

-- name: LinkArticleToCluster :execrows
INSERT INTO article_story_clusters (article_id, story_cluster_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ClusterOfArticle :one
SELECT story_cluster_id
FROM article_story_clusters
WHERE article_id = $1
LIMIT 1;

-- name: GetStoryClusterForArticle :one
SELECT sc.id, sc.title
FROM article_story_clusters AS link
JOIN story_clusters AS sc ON sc.id = link.story_cluster_id
WHERE link.article_id = $1
LIMIT 1;

-- name: ClusterIDsForArticles :many
SELECT article_id, story_cluster_id
FROM article_story_clusters
WHERE article_id = ANY(sqlc.arg(ids)::uuid[]);

-- name: MembersOfClusterForArticle :many
SELECT member.article_id
FROM article_story_clusters AS member
WHERE member.story_cluster_id = (
    SELECT cluster.story_cluster_id
    FROM article_story_clusters AS cluster
    WHERE cluster.article_id = $1
    LIMIT 1
);

-- name: TouchStoryCluster :exec
UPDATE story_clusters
SET updated_at = now()
WHERE id = $1;

-- name: GetArticleTitles :many
SELECT id, title
FROM articles
WHERE id = ANY(sqlc.arg(ids)::uuid[]);
