package storagetesting

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/kopia/repo/storage"
)

// VerifyStorage verifies the behavior of the specified storage.
func VerifyStorage(ctx context.Context, t *testing.T, r storage.Storage) {
	blocks := []struct {
		blk      string
		contents []byte
	}{
		{blk: string("abcdbbf4f0507d054ed5a80a5b65086f602b"), contents: []byte{}},
		{blk: string("zxce0e35630770c54668a8cfb4e414c6bf8f"), contents: []byte{1}},
		{blk: string("abff4585856ebf0748fd989e1dd623a8963d"), contents: bytes.Repeat([]byte{1}, 1000)},
		{blk: string("abgc3dca496d510f492c858a2df1eb824e62"), contents: bytes.Repeat([]byte{1}, 10000)},
		{blk: string("kopia.repository"), contents: bytes.Repeat([]byte{2}, 100)},
	}

	// First verify that blocks don't exist.
	for _, b := range blocks {
		AssertGetBlockNotFound(ctx, t, r, b.blk)
	}

	ctx2 := storage.WithUploadProgressCallback(ctx, func(desc string, completed, total int64) {
		log.Infof("progress %v: %v/%v", desc, completed, total)
	})

	// Now add blocks.
	for _, b := range blocks {
		if err := r.PutBlock(ctx2, b.blk, b.contents); err != nil {
			t.Errorf("can't put block: %v", err)
		}

		AssertGetBlock(ctx, t, r, b.blk, b.contents)
	}

	AssertListResults(ctx, t, r, "", blocks[0].blk, blocks[1].blk, blocks[2].blk, blocks[3].blk, blocks[4].blk)
	AssertListResults(ctx, t, r, "ab", blocks[0].blk, blocks[2].blk, blocks[3].blk)

	// Overwrite blocks.
	for _, b := range blocks {
		if err := r.PutBlock(ctx, b.blk, b.contents); err != nil {
			t.Errorf("can't put block: %v", err)
		}

		AssertGetBlock(ctx, t, r, b.blk, b.contents)
	}

	if err := r.DeleteBlock(ctx, blocks[0].blk); err != nil {
		t.Errorf("unable to delete block: %v", err)
	}
	if err := r.DeleteBlock(ctx, blocks[0].blk); err != nil {
		t.Errorf("invalid error when deleting deleted block: %v", err)
	}
	AssertListResults(ctx, t, r, "ab", blocks[2].blk, blocks[3].blk)
	AssertListResults(ctx, t, r, "", blocks[1].blk, blocks[2].blk, blocks[3].blk, blocks[4].blk)
}

// AssertConnectionInfoRoundTrips verifies that the ConnectionInfo returned by a given storage can be used to create
// equivalent storage
func AssertConnectionInfoRoundTrips(ctx context.Context, t *testing.T, s storage.Storage) {
	t.Helper()

	ci := s.ConnectionInfo()
	s2, err := storage.NewStorage(ctx, ci)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	ci2 := s2.ConnectionInfo()
	if !reflect.DeepEqual(ci, ci2) {
		t.Errorf("connection info does not round-trip: %v vs %v", ci, ci2)
	}

	if err := s2.Close(ctx); err != nil {
		t.Errorf("unable to close storage: %v", err)
	}
}
