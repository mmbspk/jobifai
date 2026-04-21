package config

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

const machineKeyDBKey = "machine_key"
const machineKeyUserID = "__system__"

// MachineKey returns the persistent machine encryption key stored in the
// settings table, generating and saving one on first run.
func MachineKey(db *sql.DB) (string, error) {
	var key string
	err := db.QueryRow(
		"SELECT value FROM settings WHERE user_id = ? AND key = ?",
		machineKeyUserID, machineKeyDBKey,
	).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return "", fmt.Errorf("generate machine key: %w", err)
		}
		key = hex.EncodeToString(b)
		_, err = db.Exec(
			`INSERT INTO settings(user_id, key, value) VALUES(?,?,?)
			 ON CONFLICT(user_id, key) DO NOTHING`,
			machineKeyUserID, machineKeyDBKey, key,
		)
		if err != nil {
			return "", fmt.Errorf("save machine key: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("load machine key: %w", err)
	}
	return key, nil
}
