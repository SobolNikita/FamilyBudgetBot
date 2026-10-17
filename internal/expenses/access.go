package expenses

import "fmt"

// FamilyActor must be selected explicitly by the public family API.
// A missing (zero) actor never grants access to another participant's data.
const FamilyActor int64 = -1

func ValidateActor(actor int64) error {
	if actor <= 0 && actor != FamilyActor {
		return fmt.Errorf("%w: не задан участник операции", ErrInvalid)
	}
	return nil
}
