package services

import (
	"encoding/json"
	"errors"

	"lumen/internal/compress"
	"lumen/internal/fileops"
	"lumen/internal/index"
	"lumen/internal/media"
	"lumen/internal/search"
	"lumen/internal/signin"
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
	CodeTrashChanged    ErrorCode = "trash_changed"    // the Trash changed after Delete from disk was confirmed
	CodeXMPOff          ErrorCode = "xmp_off"          // XMP sidecars were asked for while the setting is off

	CodeInvalidTemplate  ErrorCode = "invalid_template"  // the folder scheme to organise by cannot be used
	CodeOrganiseBusy     ErrorCode = "organise_busy"     // files are already being moved
	CodeOrganiseState    ErrorCode = "organise_state"    // that run cannot be started or undone now
	CodeSelectionChanged ErrorCode = "selection_changed" // select-all or range membership changed
	CodeStopping         ErrorCode = "stopping"          // Lumen is closing

	CodeInvalidFileName     ErrorCode = "invalid_file_name"    // Windows cannot give a file that name
	CodeDestinationOutside  ErrorCode = "destination_outside"  // a move leaves its source folder, or changes the format
	CodeDestinationFolder   ErrorCode = "destination_folder"   // a link or a file is where a folder must be
	CodeDestinationExcluded ErrorCode = "destination_excluded" // the folder is one Lumen does not index
	CodeDestinationBlocked  ErrorCode = "destination_blocked"  // the folder is blocked
	CodeDestinationTaken    ErrorCode = "destination_taken"    // something else has that name
	CodeSourceChanged       ErrorCode = "source_changed"       // the file changed after the preview
	CodeUnsupported         ErrorCode = "unsupported"          // nothing here can read or write that format

	CodePresetInvalid    ErrorCode = "preset_invalid" // not a customisation file Lumen wrote
	CodePresetNewer      ErrorCode = "preset_newer"   // a customisation file from a later Lumen
	CodeCompressionBusy  ErrorCode = "compression_busy"
	CodeCompressionState ErrorCode = "compression_state"
	CodeCompressionPlan  ErrorCode = "compression_plan"
	CodePreservation     ErrorCode = "preservation"
	CodeCopySpace        ErrorCode = "copy_space"

	CodeSigninFailed ErrorCode = "signin_failed"      // the Windows sign-in was cancelled, or wrong
	CodeNoSignin     ErrorCode = "signin_unavailable" // this PC has no way to check a Windows sign-in
)

// ErrFolderNotFound means a folder to add does not exist (or is a file).
var ErrFolderNotFound = errors.New("folder not found")

// errorCodes maps errors to their codes, the more specific first (an
// existing album's name is also an invalid album).
var errorCodes = []struct {
	err  error
	code ErrorCode
}{
	{ErrTrashChanged, CodeTrashChanged}, // before not_found: an asset that left the Trash changed it
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
	{index.ErrXMPOff, CodeXMPOff},
	{fileops.ErrInvalidTemplate, CodeInvalidTemplate},
	{ErrOrganiseBusy, CodeOrganiseBusy},
	{ErrOrganiseState, CodeOrganiseState},
	{store.ErrSelectionChanged, CodeSelectionChanged},
	{ErrOrganiseStopping, CodeStopping},
	{fileops.ErrInvalidFileName, CodeInvalidFileName},
	{fileops.ErrDestinationOutside, CodeDestinationOutside},
	{fileops.ErrDestinationFolder, CodeDestinationFolder},
	{fileops.ErrDestinationExcluded, CodeDestinationExcluded},
	{fileops.ErrDestinationBlocked, CodeDestinationBlocked},
	{fileops.ErrExists, CodeDestinationTaken},
	{fileops.ErrSourceChanged, CodeSourceChanged},
	{media.ErrUnsupported, CodeUnsupported},
	{compress.ErrUnsupported, CodeUnsupported},
	{compress.ErrPreservation, CodePreservation},
	{compress.ErrPlan, CodeCompressionPlan},
	{fileops.ErrCopySpace, CodeCopySpace},
	{ErrCompressionBusy, CodeCompressionBusy},
	{ErrCompressionState, CodeCompressionState},
	{ErrPresetInvalid, CodePresetInvalid},
	{ErrPresetNewer, CodePresetNewer},
	{signin.ErrNotVerified, CodeSigninFailed},
	{signin.ErrUnavailable, CodeNoSignin},
}

// Failure is what a failed call gives the UI besides its message (the
// error's cause, in the runtime's terms).
type Failure struct {
	Code ErrorCode `json:"code"`
}

// MarshalError is the application's error marshaller: failures with a code
// reach the UI as {"code": ...}; for others Wails sends only the message.
func MarshalError(err error) []byte {
	code := errorCodeOf(err)
	if code == "" {
		return nil
	}
	b, jerr := json.Marshal(Failure{Code: code})
	if jerr != nil {
		return nil
	}
	return b
}

// errorCodeOf is the code of a failure, "" for one the UI has no words for.
func errorCodeOf(err error) ErrorCode {
	for _, c := range errorCodes {
		if errors.Is(err, c.err) {
			return c.code
		}
	}
	return ""
}
