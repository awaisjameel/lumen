package store

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
)

var (
	// ErrPINExists means a PIN has already been established.
	ErrPINExists = errors.New("a Blocked PIN already exists")
	// ErrInvalidPIN means the PIN is invalid or does not match.
	ErrInvalidPIN = errors.New("incorrect PIN")
)

const pinIterations = 600_000

func validPIN(pin string) bool {
	if len(pin) < 6 || len(pin) > 12 {
		return false
	}
	for _, c := range pin {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// HasBlockPIN reports whether a PIN was created for the Blocked screen.
func (s *Store) HasBlockPIN(ctx context.Context) (bool, error) {
	var exists bool
	err := s.r.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM settings WHERE key = 'block_pin_hash')").Scan(&exists)
	return exists, err
}

// CreateBlockPIN stores a salted, deliberately slow PIN hash. It never stores
// the PIN itself. The two settings are committed together.
func (s *Store) CreateBlockPIN(ctx context.Context, pin string) error {
	if !validPIN(pin) {
		return ErrInvalidPIN
	}
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	hash, err := pbkdf2.Key(sha256.New, pin, salt, pinIterations, 32)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM settings WHERE key = 'block_pin_hash')").Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrPINExists
		}
		for key, value := range map[string]string{"block_pin_salt": hex.EncodeToString(salt), "block_pin_hash": hex.EncodeToString(hash)} {
			if _, err := tx.ExecContext(ctx, "INSERT INTO settings(key, value) VALUES (?, ?)", key, value); err != nil {
				return err
			}
		}
		return nil
	})
}

// VerifyBlockPIN compares in constant time. The service limits attempts.
func (s *Store) VerifyBlockPIN(ctx context.Context, pin string) (bool, error) {
	if !validPIN(pin) {
		return false, nil
	}
	var saltHex, hashHex string
	err := s.read(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'block_pin_salt'").Scan(&saltHex); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'block_pin_hash'").Scan(&hashHex)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return false, err
	}
	want, err := hex.DecodeString(hashHex)
	if err != nil {
		return false, err
	}
	if len(salt) != 32 || len(want) != 32 {
		return false, ErrInvalidPIN
	}
	got, err := pbkdf2.Key(sha256.New, pin, salt, pinIterations, len(want))
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1, err
}
