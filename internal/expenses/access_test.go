package expenses

import (
	"context"
	"errors"
	"testing"
)

func TestMissingActorCannotGrantFamilyAccess(t *testing.T) {
	// A nil repository makes any accidental delegation fail immediately.
	service := Service{}
	ctx := context.Background()
	input := Input{UserID: 111, Amount: "10", Category: "еда", SpentAt: "2026-10-17", Version: 1}
	for _, actor := range []int64{0, -2} {
		_, createErr := service.Create(ctx, actor, input)
		_, updateErr := service.Update(ctx, actor, 1, input)
		deleteErr := service.Delete(ctx, actor, 1, 1)
		_, listErr := service.List(ctx, actor, Filter{})
		_, summaryErr := service.Summary(ctx, actor, Filter{})
		for _, err := range []error{createErr, updateErr, deleteErr, listErr, summaryErr} {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("actor %d accepted: %v", actor, err)
			}
		}
	}
	if err := ValidateActor(FamilyActor); err != nil {
		t.Fatal(err)
	}
}
