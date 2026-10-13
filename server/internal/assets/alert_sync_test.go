package assets

import (
	"context"
	"errors"
	"testing"

	"github.com/Rahmannugar/macro-terminal/server/internal/common/paging"
	entitymodels "github.com/Rahmannugar/macro-terminal/server/internal/entities/models"
	"github.com/google/uuid"
)

type stubWatchList struct {
	subscribeErr   error
	unsubscribeErr error
	gotSubscribe   uuid.UUID
	gotUnsubscribe uuid.UUID
}

func (stub *stubWatchList) PairsForUserPage(context.Context, uuid.UUID, *paging.Cursor, int32) ([]entitymodels.EntityPair, *paging.Cursor, error) {
	return nil, nil, nil
}

func (stub *stubWatchList) Subscribe(_ context.Context, _ uuid.UUID, entityPairID uuid.UUID) error {
	stub.gotSubscribe = entityPairID
	return stub.subscribeErr
}

func (stub *stubWatchList) Unsubscribe(_ context.Context, _ uuid.UUID, entityPairID uuid.UUID) error {
	stub.gotUnsubscribe = entityPairID
	return stub.unsubscribeErr
}

type stubAlertSyncer struct {
	subscribeErr   error
	unsubscribeErr error
	gotSubscribe   uuid.UUID
	gotUnsubscribe uuid.UUID
}

func (stub *stubAlertSyncer) SubscribePairAlerts(_ context.Context, _ uuid.UUID, entityPairID uuid.UUID) error {
	stub.gotSubscribe = entityPairID
	return stub.subscribeErr
}

func (stub *stubAlertSyncer) UnsubscribePairAlerts(_ context.Context, _ uuid.UUID, entityPairID uuid.UUID) error {
	stub.gotUnsubscribe = entityPairID
	return stub.unsubscribeErr
}

func TestWithAlertSyncSubscribesAlertsAfterFollow(t *testing.T) {
	inner := &stubWatchList{}
	syncer := &stubAlertSyncer{}
	watchList := WithAlertSync(inner, syncer)
	pairID := uuid.New()

	if err := watchList.Subscribe(t.Context(), uuid.New(), pairID); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if inner.gotSubscribe != pairID {
		t.Errorf("inner pair = %s, want %s", inner.gotSubscribe, pairID)
	}
	if syncer.gotSubscribe != pairID {
		t.Errorf("alert pair = %s, want %s", syncer.gotSubscribe, pairID)
	}
}

func TestWithAlertSyncUnsubscribesAlertsAfterUnfollow(t *testing.T) {
	inner := &stubWatchList{}
	syncer := &stubAlertSyncer{}
	watchList := WithAlertSync(inner, syncer)
	pairID := uuid.New()

	if err := watchList.Unsubscribe(t.Context(), uuid.New(), pairID); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	if inner.gotUnsubscribe != pairID {
		t.Errorf("inner pair = %s, want %s", inner.gotUnsubscribe, pairID)
	}
	if syncer.gotUnsubscribe != pairID {
		t.Errorf("alert pair = %s, want %s", syncer.gotUnsubscribe, pairID)
	}
}

func TestWithAlertSyncSkipsAlertsWhenFollowFails(t *testing.T) {
	inner := &stubWatchList{subscribeErr: errors.New("pair missing")}
	syncer := &stubAlertSyncer{}
	watchList := WithAlertSync(inner, syncer)

	if err := watchList.Subscribe(t.Context(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("subscribe succeeded, want the inner error")
	}
	if syncer.gotSubscribe != uuid.Nil {
		t.Errorf("alert pair = %s, want no alert sync after a failed follow", syncer.gotSubscribe)
	}
}

func TestWithAlertSyncReportsAlertSyncFailure(t *testing.T) {
	syncer := &stubAlertSyncer{subscribeErr: errors.New("alerts down")}
	watchList := WithAlertSync(&stubWatchList{}, syncer)

	if err := watchList.Subscribe(t.Context(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("subscribe succeeded, want the alert sync error")
	}
}
