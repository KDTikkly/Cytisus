package ledger

import (
	"crypto/rand"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

func newUUID() (pgtype.UUID, error) {
	identifier := pgtype.UUID{Valid: true}
	if _, err := rand.Read(identifier.Bytes[:]); err != nil {
		return pgtype.UUID{}, fmt.Errorf("generate UUID: %w", err)
	}
	identifier.Bytes[6] = (identifier.Bytes[6] & 0x0f) | 0x40
	identifier.Bytes[8] = (identifier.Bytes[8] & 0x3f) | 0x80
	return identifier, nil
}

func parseUUID(value string) (pgtype.UUID, error) {
	var identifier pgtype.UUID
	if err := identifier.Scan(value); err != nil || !identifier.Valid {
		if err == nil {
			err = fmt.Errorf("UUID is not valid")
		}
		return pgtype.UUID{}, err
	}
	return identifier, nil
}
