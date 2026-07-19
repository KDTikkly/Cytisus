package outbox

import (
	"regexp"

	"github.com/jackc/pgx/v5/pgtype"
)

var codePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,63}$`)

func parseEventID(value string) (pgtype.UUID, error) {
	var identifier pgtype.UUID
	if err := identifier.Scan(value); err != nil {
		return pgtype.UUID{}, err
	}
	if !identifier.Valid {
		return pgtype.UUID{}, ErrInvalidConfiguration
	}
	return identifier, nil
}
