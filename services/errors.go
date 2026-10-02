package services

import (
	"encoding/json"
	"errors"

	"lumen/internal/search"
	"lumen/internal/store"
)

// ErrorCode names a failure the UI explains in its own words (and can act
// on: "someone already has that name" asks whether they are one person).
// frontend/src/bridge mirrors the codes; services/contract_test.go checks.
type ErrorCode string

// The error codes.
const (
	CodeNotFound        ErrorCode = "not_found"    // it was deleted, or its file is gone
	CodeNoAI            ErrorCode = "no_ai"        // the on-device AI isn't available
	CodeInvalidName     ErrorCode = "invalid_name" // empty, or too long
	CodeAlbumExists     ErrorCode = "album_exists" // another album has that name
	CodeInvalidAlbum    ErrorCode = "invalid_album"
	CodeAlbumMediaKind  ErrorCode = "album_media_kind"
	CodeAlbumSelection  ErrorCode = "album_selection"
	CodeAlbumDate       ErrorCode = "album_date"
	CodeAlbumDateOrder  ErrorCode = "album_date_order"
	CodeAlbumSmartItems ErrorCode = "album_smart_items"
	CodeAlbumKind       ErrorCode = "album_kind"
	CodeAlbumReference  ErrorCode = "album_reference"
	CodeAlbumManualOnly ErrorCode = "album_manual_only"
	CodePersonExists    ErrorCode = "person_exists"   // someone else has that name
	CodeBlockedName     ErrorCode = "blocked_name"    // a name belongs to a blocked person or Thing
	CodeTagExists       ErrorCode = "tag_exists"      // another Thing has that name
	CodeBuiltinTag      ErrorCode = "builtin_tag"     // built-in Things can be blocked, not deleted
	CodeTagInAlbum      ErrorCode = "tag_in_album"    // a smart album's rule uses the Thing
	CodeAlreadyCovered  ErrorCode = "already_covered" // the folder is already in the library
	CodeFolderNotFound  ErrorCode = "folder_not_found"
	CodeInvalidPIN      ErrorCode = "invalid_pin"      // not 6 to 12 digits, or not the PIN
	CodePINExists       ErrorCode = "pin_exists"       // a PIN was already created
	CodePINRateLimited  ErrorCode = "pin_rate_limited" // too many wrong PINs: wait a moment
	CodeBlocksLocked    ErrorCode = "blocks_locked"    // the Blocked list needs the PIN first
	CodeUndoExpired     ErrorCode = "undo_expired"     // too late to undo that block
	CodeInvalidBlock    ErrorCode = "invalid_block"    // that can't be blocked
	CodeDeleteFailed    ErrorCode = "delete_failed"    // the Recycle Bin could not be used
)

// ErrFolderNotFound means a folder to add does not exist (or is a file).
var ErrFolderNotFound = errors.New("folder not found")

// errorCodes maps errors to their codes, the more specific first (an
// existing album's name is also an invalid album).
var errorCodes = []struct {
	err  error
	code ErrorCode
}{
	{store.ErrNotFound, CodeNotFound},
	{ErrNoAI, CodeNoAI},
	{search.ErrNotLoaded, CodeNoAI},
	{search.ErrClosed, CodeNoAI},
	{store.ErrInvalidName, CodeInvalidName},
	{store.ErrAlbumExists, CodeAlbumExists},
	{store.ErrAlbumMediaKind, CodeAlbumMediaKind},
	{store.ErrAlbumSelection, CodeAlbumSelection},
	{store.ErrAlbumDate, CodeAlbumDate},
	{store.ErrAlbumDateOrder, CodeAlbumDateOrder},
	{store.ErrAlbumSmartItems, CodeAlbumSmartItems},
	{store.ErrAlbumKind, CodeAlbumKind},
	{store.ErrAlbumReference, CodeAlbumReference},
	{store.ErrAlbumManualOnly, CodeAlbumManualOnly},
	{store.ErrInvalidAlbum, CodeInvalidAlbum},
	{store.ErrPersonExists, CodePersonExists},
	{store.ErrBlockedName, CodeBlockedName},
	{store.ErrTagExists, CodeTagExists},
	{store.ErrBuiltinTag, CodeBuiltinTag},
	{store.ErrTagInAlbum, CodeTagInAlbum},
	{store.ErrAlreadyCovered, CodeAlreadyCovered},
	{ErrFolderNotFound, CodeFolderNotFound},
	{store.ErrInvalidPIN, CodeInvalidPIN},
	{store.ErrPINExists, CodePINExists},
	{ErrPINRateLimited, CodePINRateLimited},
	{ErrBlockLocked, CodeBlocksLocked},
	{store.ErrBlockChangeLocked, CodeBlocksLocked},
	{ErrBlockUndoExpired, CodeUndoExpired},
	{store.ErrInvalidBlock, CodeInvalidBlock},
	{ErrDeleteFailed, CodeDeleteFailed},
}

// Failure is what a failed call gives the UI besides its message (the
// error's cause, in the runtime's terms).
type Failure struct {
	Code ErrorCode `json:"code"`
}

// MarshalError is the application's error marshaller: failures with a code
// reach the UI as {"code": ...}; for others Wails sends only the message.
func MarshalError(err error) []byte {
	for _, c := range errorCodes {
		if errors.Is(err, c.err) {
			b, jerr := json.Marshal(Failure{Code: c.code})
			if jerr == nil {
				return b
			}
		}
	}
	return nil
}
