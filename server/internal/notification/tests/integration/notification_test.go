//go:build integration

package integration_test

import (
	"testing"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/ids"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database/testdb"
	notificationrepositories "github.com/Rahmannugar/macro-terminal/server/internal/notification/repositories"
	sourcemodels "github.com/Rahmannugar/macro-terminal/server/internal/sources/models"
	sourcerepositories "github.com/Rahmannugar/macro-terminal/server/internal/sources/repositories"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestArticleNotificationFanOut(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)
	repository := notificationrepositories.NewRepository(pool)

	sourceRepository := sourcerepositories.NewSourceRepository(pool)
	source, err := sourceRepository.UpsertSource(t.Context(), sourcemodels.Source{
		ID: testID(t), Name: "Notification Test Source", Type: "news",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	usd := seedEntity(t, pool, "USD", "United States Dollar", "currency")
	gbp := seedEntity(t, pool, "GBP", "British Pound", "currency")
	eur := seedEntity(t, pool, "EUR", "Euro", "currency")
	subscribedPair := seedPair(t, pool, gbp, usd, "GBP/USD")
	unrelatedPair := seedPair(t, pool, eur, gbp, "EUR/GBP")

	subscriber := seedUser(t, pool, "notify-subscriber")
	killSwitched := seedUser(t, pool, "notify-kill-switched")
	seedAlert(t, pool, subscriber, "new_article", subscribedPair, "in-app")
	seedAlert(t, pool, subscriber, "new_article", unrelatedPair, "in-app")
	seedAlert(t, pool, killSwitched, "new_article", subscribedPair, "in-app")
	seedChannel(t, pool, subscriber, "in-app")

	articleID := seedArticle(t, pool, source.ID, "Fed holds rates steady", "https://example.test/notify-a")
	seedArticleEntity(t, pool, articleID, usd)

	queued, err := repository.EnqueueMissingArticleNotifications(t.Context(), 16)
	if err != nil {
		t.Fatalf("enqueue article notifications: %v", err)
	}
	if queued != 1 {
		t.Fatalf("first enqueue = %d, want 1 (the mapped article)", queued)
	}
	queued, err = repository.EnqueueMissingArticleNotifications(t.Context(), 16)
	if err != nil {
		t.Fatalf("enqueue repeat: %v", err)
	}
	if queued != 0 {
		t.Fatalf("repeat enqueue = %d, want 0 (the queued row blocks requeueing)", queued)
	}

	claimed, err := repository.ClaimBatch(t.Context(), 16)
	if err != nil {
		t.Fatalf("claim batch: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed = %d, want 1", len(claimed))
	}
	work := claimed[0]
	if work.SubjectType != "article" || work.SubjectID != articleID || work.Attempts != 1 {
		t.Fatalf("claimed = %+v, want the article subject on attempt 1", work)
	}

	subject, err := repository.ArticleSubject(t.Context(), work.SubjectID)
	if err != nil {
		t.Fatalf("article subject: %v", err)
	}
	if subject.Title != "Fed holds rates steady" {
		t.Errorf("subject title = %q, want the seeded headline", subject.Title)
	}

	created, err := repository.FanOutArticle(t.Context(), subject.ID, subject.Title)
	if err != nil {
		t.Fatalf("fan out article: %v", err)
	}
	if created != 1 {
		t.Fatalf("fan-out created = %d, want 1 (only the alert whose channel is enabled, only the affected pair)", created)
	}
	repeated, err := repository.FanOutArticle(t.Context(), subject.ID, subject.Title)
	if err != nil {
		t.Fatalf("repeat fan out: %v", err)
	}
	if repeated != 0 {
		t.Fatalf("repeat fan-out = %d, want 0 (notifications are deduplicated)", repeated)
	}

	if err := repository.Complete(t.Context(), work.ID); err != nil {
		t.Fatalf("complete: %v", err)
	}
	queued, err = repository.EnqueueMissingArticleNotifications(t.Context(), 16)
	if err != nil {
		t.Fatalf("enqueue after complete: %v", err)
	}
	if queued != 0 {
		t.Fatalf("enqueue after complete = %d, want 0 (the done row blocks requeueing)", queued)
	}

	var stored struct {
		UserID  uuid.UUID
		Type    string
		PairID  uuid.UUID
		Channel string
		Title   string
	}
	err = pool.QueryRow(t.Context(),
		`SELECT user_id, type, entity_pair_id, channel, title
		 FROM notifications
		 WHERE subject_type = 'article' AND subject_id = $1`,
		articleID).Scan(&stored.UserID, &stored.Type, &stored.PairID, &stored.Channel, &stored.Title)
	if err != nil {
		t.Fatalf("read notification: %v", err)
	}
	if stored.UserID != subscriber || stored.Type != "new_article" ||
		stored.PairID != subscribedPair || stored.Channel != "in-app" ||
		stored.Title != "Fed holds rates steady" {
		t.Errorf("notification = %+v, want the subscriber's in-app new_article record on GBP/USD", stored)
	}

	var killSwitchedCount int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM notifications WHERE user_id = $1`, killSwitched).
		Scan(&killSwitchedCount); err != nil {
		t.Fatalf("count kill-switched notifications: %v", err)
	}
	if killSwitchedCount != 0 {
		t.Errorf("kill-switched notifications = %d, want 0 (no configured channel row)", killSwitchedCount)
	}
}

func TestCalendarEventNotificationFanOut(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)
	repository := notificationrepositories.NewRepository(pool)

	sourceRepository := sourcerepositories.NewSourceRepository(pool)
	source, err := sourceRepository.UpsertSource(t.Context(), sourcemodels.Source{
		ID: testID(t), Name: "Calendar Notification Test Source", Type: "economic_calendar",
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}

	usd := seedEntity(t, pool, "USD", "United States Dollar", "currency")
	gbp := seedEntity(t, pool, "GBP", "British Pound", "currency")
	pair := seedPair(t, pool, gbp, usd, "GBP/USD")

	subscriber := seedUser(t, pool, "notify-event-subscriber")
	seedAlert(t, pool, subscriber, "new_calendar_event", pair, "in-app")
	seedChannel(t, pool, subscriber, "in-app")

	indicatorID := testID(t)
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO economic_indicators (id, name, entity_id, type)
		 VALUES ($1, 'CPI', $2, 'inflation')`, indicatorID, usd); err != nil {
		t.Fatalf("insert indicator: %v", err)
	}
	eventID := testID(t)
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO calendar_events (id, source_id, indicator_id, scheduled_at)
		 VALUES ($1, $2, $3, now() + interval '3 days')`, eventID, source.ID, indicatorID); err != nil {
		t.Fatalf("insert calendar event: %v", err)
	}
	seedCalendarEventEntity(t, pool, eventID, usd)

	queued, err := repository.EnqueueMissingCalendarEventNotifications(t.Context(), 16)
	if err != nil {
		t.Fatalf("enqueue event notifications: %v", err)
	}
	if queued != 1 {
		t.Fatalf("first enqueue = %d, want 1 (the mapped event)", queued)
	}

	claimed, err := repository.ClaimBatch(t.Context(), 16)
	if err != nil {
		t.Fatalf("claim batch: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed = %d, want 1", len(claimed))
	}
	work := claimed[0]
	if work.SubjectType != "calendar_event" || work.SubjectID != eventID {
		t.Fatalf("claimed = %+v, want the calendar event subject", work)
	}

	subject, err := repository.CalendarEventSubject(t.Context(), work.SubjectID)
	if err != nil {
		t.Fatalf("calendar event subject: %v", err)
	}
	if subject.Title != "CPI" {
		t.Errorf("subject title = %q, want the indicator name", subject.Title)
	}

	created, err := repository.FanOutCalendarEvent(t.Context(), subject.ID, subject.Title)
	if err != nil {
		t.Fatalf("fan out calendar event: %v", err)
	}
	if created != 1 {
		t.Fatalf("fan-out created = %d, want 1", created)
	}

	var storedType, storedChannel string
	err = pool.QueryRow(t.Context(),
		`SELECT type, channel FROM notifications
		 WHERE subject_type = 'calendar_event' AND subject_id = $1`, eventID).
		Scan(&storedType, &storedChannel)
	if err != nil {
		t.Fatalf("read notification: %v", err)
	}
	if storedType != "new_calendar_event" || storedChannel != "in-app" {
		t.Errorf("notification type/channel = %q/%q, want new_calendar_event/in-app", storedType, storedChannel)
	}
}

func TestMissingNotificationSubjectFailsPermanently(t *testing.T) {
	pool := testdb.OpenMigratedDatabase(t)
	repository := notificationrepositories.NewRepository(pool)

	missingID := testID(t)
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO outbox (id, type, payload)
		 VALUES ($1, 'asset_notification',
	             jsonb_build_object('subject_type', 'article', 'subject_id', $2::text))`,
		testID(t), missingID.String()); err != nil {
		t.Fatalf("insert outbox row: %v", err)
	}

	claimed, err := repository.ClaimBatch(t.Context(), 16)
	if err != nil {
		t.Fatalf("claim batch: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed = %d, want 1", len(claimed))
	}
	if _, err := repository.ArticleSubject(t.Context(), missingID); err == nil {
		t.Fatal("article subject succeeded, want ErrNoRows for the missing subject")
	}
	if err := repository.FailPermanently(t.Context(), claimed[0].ID, "article not found"); err != nil {
		t.Fatalf("fail permanently: %v", err)
	}

	var status, lastError string
	if err := pool.QueryRow(t.Context(),
		`SELECT status, coalesce(last_error, '') FROM outbox WHERE id = $1`, claimed[0].ID).
		Scan(&status, &lastError); err != nil {
		t.Fatalf("read outbox row: %v", err)
	}
	if status != "failed" || lastError != "article not found" {
		t.Errorf("outbox = %s/%q, want failed/article not found", status, lastError)
	}
}

func seedEntity(t *testing.T, pool *pgxpool.Pool, code, name, entityType string) uuid.UUID {
	t.Helper()
	id := testID(t)
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO entities (id, code, name, type) VALUES ($1, $2, $3, $4)`,
		id, code, name, entityType); err != nil {
		t.Fatalf("insert entity %s: %v", code, err)
	}
	return id
}

func seedPair(t *testing.T, pool *pgxpool.Pool, baseID, quoteID uuid.UUID, symbol string) uuid.UUID {
	t.Helper()
	id := testID(t)
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO entity_pairs (id, base_entity_id, quote_entity_id, symbol)
		 VALUES ($1, $2, $3, $4)`, id, baseID, quoteID, symbol); err != nil {
		t.Fatalf("insert pair %s: %v", symbol, err)
	}
	return id
}

func seedUser(t *testing.T, pool *pgxpool.Pool, username string) uuid.UUID {
	t.Helper()
	id := testID(t)
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO users (id, username, authlier_subject_id, role)
		 VALUES ($1, $2, $3, 'user')`, id, username, username+"-subject"); err != nil {
		t.Fatalf("insert user %s: %v", username, err)
	}
	return id
}

func seedAlert(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, alertType string, pairID uuid.UUID, channel string) {
	t.Helper()
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO user_alerts (id, user_id, type, entity_pair_id, channel)
		 VALUES ($1, $2, $3, $4, $5)`, testID(t), userID, alertType, pairID, channel); err != nil {
		t.Fatalf("insert alert: %v", err)
	}
}

func seedChannel(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, channel string) {
	t.Helper()
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO user_notification_channels (id, user_id, channel)
		 VALUES ($1, $2, $3)`, testID(t), userID, channel); err != nil {
		t.Fatalf("insert notification channel: %v", err)
	}
}

func seedArticle(t *testing.T, pool *pgxpool.Pool, sourceID uuid.UUID, title, url string) uuid.UUID {
	t.Helper()
	id := testID(t)
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO articles (id, source_id, title, content, url, published_at)
		 VALUES ($1, $2, $3, $4, $5, now() - interval '1 hour')`,
		id, sourceID, title, "Article body for notification tests.", url); err != nil {
		t.Fatalf("insert article: %v", err)
	}
	return id
}

func seedArticleEntity(t *testing.T, pool *pgxpool.Pool, articleID, entityID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO article_entities (article_id, entity_id) VALUES ($1, $2)`,
		articleID, entityID); err != nil {
		t.Fatalf("insert article entity: %v", err)
	}
}

func seedCalendarEventEntity(t *testing.T, pool *pgxpool.Pool, eventID, entityID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO calendar_event_entities (calendar_event_id, entity_id) VALUES ($1, $2)`,
		eventID, entityID); err != nil {
		t.Fatalf("insert calendar event entity: %v", err)
	}
}

func testID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := ids.New()
	if err != nil {
		t.Fatalf("generate ID: %v", err)
	}
	return id
}
