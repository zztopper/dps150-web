package history

import (
	"context"
	"errors"
	"testing"

	"dps150-web/backend/internal/storage"
)

func TestReaderRawRangeOrderAndLimit(t *testing.T) {
	t.Parallel()

	s := openStorage(t)
	ctx := context.Background()
	insertSamples(t, s, []Sample{
		rawSample(1_000, 1), rawSample(2_000, 2),
		rawSample(3_000, 3), rawSample(4_000, 4),
	})
	r := NewReader(s)

	// Bounds are inclusive on both ends.
	items, err := r.Raw(ctx, 2_000, 3_000, 0)
	if err != nil {
		t.Fatalf("Raw: %v", err)
	}
	if len(items) != 2 || items[0].TS != 2_000 || items[1].TS != 3_000 {
		t.Errorf("Raw(2000..3000) = %+v, want ts 2000, 3000", items)
	}

	// Limit caps the result, keeping ascending ts order.
	items, err = r.Raw(ctx, 0, 10_000, 3)
	if err != nil {
		t.Fatalf("Raw with limit: %v", err)
	}
	if len(items) != 3 || items[0].TS != 1_000 || items[2].TS != 3_000 {
		t.Errorf("Raw(limit 3) = %+v, want first three by ts", items)
	}

	// An empty window yields an empty slice, not an error.
	items, err = r.Raw(ctx, 8_000, 9_000, 0)
	if err != nil || len(items) != 0 {
		t.Errorf("Raw(empty window) = %+v, %v; want empty, nil", items, err)
	}
}

func TestReaderMinutesRangeOrderAndLimit(t *testing.T) {
	t.Parallel()

	s := openStorage(t)
	ctx := context.Background()
	insertMinutes(t, s, []Sample1m{
		{TS: 60_000, Cnt: 10}, {TS: 120_000, Cnt: 20}, {TS: 180_000, Cnt: 30},
	})
	r := NewReader(s)

	items, err := r.Minutes(ctx, 60_000, 120_000, 0)
	if err != nil {
		t.Fatalf("Minutes: %v", err)
	}
	if len(items) != 2 || items[0].TS != 60_000 || items[1].TS != 120_000 {
		t.Errorf("Minutes(60000..120000) = %+v, want ts 60000, 120000", items)
	}

	items, err = r.Minutes(ctx, 0, 200_000, 1)
	if err != nil {
		t.Fatalf("Minutes with limit: %v", err)
	}
	if len(items) != 1 || items[0].TS != 60_000 {
		t.Errorf("Minutes(limit 1) = %+v, want the first bucket", items)
	}
}

func TestReaderUnavailable(t *testing.T) {
	t.Parallel()

	// A Reader over a nil storage (disabled by configuration) must report
	// ErrUnavailable, never panic.
	r := NewReader(nil)
	if _, err := r.Raw(context.Background(), 0, 1, 0); !errors.Is(err, storage.ErrUnavailable) {
		t.Errorf("Raw over nil storage: err = %v, want ErrUnavailable", err)
	}
	if _, err := r.Minutes(context.Background(), 0, 1, 0); !errors.Is(err, storage.ErrUnavailable) {
		t.Errorf("Minutes over nil storage: err = %v, want ErrUnavailable", err)
	}
}
