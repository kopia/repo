package block

import (
	"context"
	"testing"
	"time"

	"github.com/kopia/repo/storage"
)

func TestBlockIndexRecovery(t *testing.T) {
	ctx := context.Background()
	data := map[string][]byte{}
	keyTime := map[string]time.Time{}
	bm := newTestBlockManager(data, keyTime, nil)
	block1 := writeBlockAndVerify(ctx, t, bm, seededRandomData(10, 100))
	block2 := writeBlockAndVerify(ctx, t, bm, seededRandomData(11, 100))
	block3 := writeBlockAndVerify(ctx, t, bm, seededRandomData(12, 100))

	if err := bm.Flush(ctx); err != nil {
		t.Errorf("flush error: %v", err)
	}

	// delete all index blocks
	assertNoError(t, bm.st.ListBlocks(ctx, newIndexBlockPrefix, func(bi storage.BlockMetadata) error {
		log.Debugf("deleting %v", bi.BlockID)
		return bm.st.DeleteBlock(ctx, bi.BlockID)
	}))

	// now with index blocks gone, all blocks appear to not be found
	bm = newTestBlockManager(data, keyTime, nil)
	verifyBlockNotFound(ctx, t, bm, block1)
	verifyBlockNotFound(ctx, t, bm, block2)
	verifyBlockNotFound(ctx, t, bm, block3)

	totalRecovered := 0

	// pass 1 - just list blocks to recover, but don't commit
	err := bm.st.ListBlocks(ctx, PackBlockPrefix, func(bi storage.BlockMetadata) error {
		infos, err := bm.RecoverIndexFromPackFile(ctx, bi.BlockID, bi.Length, false)
		if err != nil {
			return err
		}
		totalRecovered += len(infos)
		log.Debugf("recovered %v blocks", len(infos))
		return nil
	})
	if err != nil {
		t.Errorf("error recovering: %v", err)
	}

	if got, want := totalRecovered, 3; got != want {
		t.Errorf("invalid # of blocks recovered: %v, want %v", got, want)
	}

	// blocks are stil not found
	verifyBlockNotFound(ctx, t, bm, block1)
	verifyBlockNotFound(ctx, t, bm, block2)
	verifyBlockNotFound(ctx, t, bm, block3)

	// pass 2 now pass commit=true to add recovered blocks to index
	totalRecovered = 0

	err = bm.st.ListBlocks(ctx, PackBlockPrefix, func(bi storage.BlockMetadata) error {
		infos, err := bm.RecoverIndexFromPackFile(ctx, bi.BlockID, bi.Length, true)
		if err != nil {
			return err
		}
		totalRecovered += len(infos)
		log.Debugf("recovered %v blocks", len(infos))
		return nil
	})
	if err != nil {
		t.Errorf("error recovering: %v", err)
	}

	if got, want := totalRecovered, 3; got != want {
		t.Errorf("invalid # of blocks recovered: %v, want %v", got, want)
	}

	verifyBlock(ctx, t, bm, block1, seededRandomData(10, 100))
	verifyBlock(ctx, t, bm, block2, seededRandomData(11, 100))
	verifyBlock(ctx, t, bm, block3, seededRandomData(12, 100))
	if err := bm.Flush(ctx); err != nil {
		t.Errorf("flush error: %v", err)
	}
	verifyBlock(ctx, t, bm, block1, seededRandomData(10, 100))
	verifyBlock(ctx, t, bm, block2, seededRandomData(11, 100))
	verifyBlock(ctx, t, bm, block3, seededRandomData(12, 100))
}
