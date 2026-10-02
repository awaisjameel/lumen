package store

import (
	"errors"
	"reflect"
	"testing"

	"lumen/internal/core"
)

func TestReplaceBlockPINMakesTheNewPINTheOnlyOne(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	check(t, st.CreateBlockPIN(ctx, "123456"))
	var oldSalt string
	check(t, st.r.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'block_pin_salt'").Scan(&oldSalt))

	check(t, st.ReplaceBlockPIN(ctx, "654321"))

	if ok, err := st.VerifyBlockPIN(ctx, "654321"); err != nil || !ok {
		t.Fatalf("new PIN: %v, %v", ok, err)
	}
	if ok, err := st.VerifyBlockPIN(ctx, "123456"); err != nil || ok {
		t.Fatalf("old PIN after a reset: %v, %v", ok, err)
	}
	var newSalt string
	check(t, st.r.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'block_pin_salt'").Scan(&newSalt))
	if newSalt == oldSalt {
		t.Fatal("a reset kept the old salt")
	}
	// The PIN can be created only once; a reset replaces it and leaves that so.
	if err := st.CreateBlockPIN(ctx, "111111"); !errors.Is(err, ErrPINExists) {
		t.Fatalf("create after a reset: %v", err)
	}
}

func TestReplaceBlockPINKeepsEveryBlock(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\private.jpg`, "private"), item(src.ID, `C:\Photos\public.jpg`, "public")})))
	check(t, st.CreateBlockPIN(ctx, "123456"))
	_, err = st.SetBlock(ctx, core.BlockAsset, 1, "", core.BlockPhotos)
	check(t, err)
	_, err = st.SetBlock(ctx, core.BlockFolder, 0, `C:\Photos\sub`, core.BlockPhotos)
	check(t, err)
	before, err := st.Blocks(ctx)
	check(t, err)
	visible, err := st.CountVisible(ctx)
	check(t, err)
	if len(before) != 2 || visible != 1 {
		t.Fatalf("setup: %d blocks, %d visible", len(before), visible)
	}

	check(t, st.ReplaceBlockPIN(ctx, "654321"))

	after, err := st.Blocks(ctx)
	check(t, err)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("a PIN reset changed the blocks:\n%+v\nwas\n%+v", after, before)
	}
	if n, err := st.CountVisible(ctx); err != nil || n != visible {
		t.Fatalf("visible after a reset = %d, %v; want %d", n, err, visible)
	}
}

func TestReplaceBlockPINRefusesABadPINAndKeepsTheOldOne(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	check(t, st.CreateBlockPIN(ctx, "123456"))
	for _, pin := range []string{"", "12345", "1234567890123", "12345a", " 123456"} {
		if err := st.ReplaceBlockPIN(ctx, pin); !errors.Is(err, ErrInvalidPIN) {
			t.Fatalf("replace with %q: %v", pin, err)
		}
	}
	if ok, err := st.VerifyBlockPIN(ctx, "123456"); err != nil || !ok {
		t.Fatalf("old PIN after refused resets: %v, %v", ok, err)
	}
}

func TestReplaceBlockPINWithoutAPINCreatesNone(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	if err := st.ReplaceBlockPIN(ctx, "123456"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replace with no PIN: %v", err)
	}
	if configured, err := st.HasBlockPIN(ctx); err != nil || configured {
		t.Fatalf("a reset created a PIN: %v, %v", configured, err)
	}
}

func TestReplaceBlockPINWritesNothingWhenOneHalfIsMissing(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	check(t, st.CreateBlockPIN(ctx, "123456"))
	check(t, errOf(st.w.ExecContext(ctx, "DELETE FROM settings WHERE key = 'block_pin_hash'")))
	var salt string
	check(t, st.r.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'block_pin_salt'").Scan(&salt))

	if err := st.ReplaceBlockPIN(ctx, "654321"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replace with half a PIN: %v", err)
	}

	var after string
	check(t, st.r.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'block_pin_salt'").Scan(&after))
	if after != salt {
		t.Fatal("a failed reset still replaced the salt: the two settings were not written together")
	}
}
