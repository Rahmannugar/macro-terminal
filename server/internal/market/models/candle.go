package models

import (
	"time"

	"github.com/google/uuid"
)

type PersistCandle struct {
	ID           uuid.UUID
	SourceID     uuid.UUID
	EntityPairID uuid.UUID
	Timeframe    string
	Timestamp    time.Time
	Open         float64
	High         float64
	Low          float64
	Close        float64
}

type StoredCandle struct {
	ID           uuid.UUID
	EntityPairID uuid.UUID
	Timeframe    string
	Timestamp    time.Time
	Open         float64
	High         float64
	Low          float64
	Close        float64
}

type TimeWindow struct {
	From time.Time
	To   time.Time
}
