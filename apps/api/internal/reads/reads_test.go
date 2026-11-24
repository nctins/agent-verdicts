package reads

import (
	"errors"
	"testing"
	"time"
)

func TestDetailFromAndValidate(t *testing.T) {
	today := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	if got := DetailFrom(today, 180); !got.Equal(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("DetailFrom = %v", got)
	}
	for _, d := range []int{MinDetailDays, DefaultDetailDays, MaxDetailDays} {
		if err := ValidateDetailDays(d); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []int{MinDetailDays - 1, MaxDetailDays + 1} {
		if err := ValidateDetailDays(d); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("%d: %v", d, err)
		}
	}
}
