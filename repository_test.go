package repo_test

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"io/ioutil"
	"math/rand"
	"reflect"
	"runtime/debug"
	"testing"

	"github.com/kopia/repo"
	"github.com/kopia/repo/block"
	"github.com/kopia/repo/internal/repotesting"
	"github.com/kopia/repo/object"
	"github.com/kopia/repo/storage"
)

func TestWriters(t *testing.T) {
	cases := []struct {
		data     []byte
		objectID object.ID
	}{
		{
			[]byte("the quick brown fox jumps over the lazy dog"),
			"345acef0bcf82f1daf8e49fab7b7fac7ec296c518501eabea3645b99345a4e08",
		},
		{make([]byte, 100), "1d804f1f69df08f3f59070bf962de69433e3d61ac18522a805a84d8c92741340"}, // 100 zero bytes
	}

	ctx := context.Background()

	for _, c := range cases {
		var env repotesting.Environment
		defer env.Setup(t).Close(t)

		writer := env.Repository.Objects.NewWriter(ctx, object.WriterOptions{})
		if _, err := writer.Write(c.data); err != nil {
			t.Fatalf("write error: %v", err)
		}

		result, err := writer.Result()
		if err != nil {
			t.Errorf("error getting writer results for %v, expected: %v", c.data, c.objectID.String())
			continue
		}

		if !objectIDsEqual(result, c.objectID) {
			t.Errorf("incorrect result for %v, expected: %v got: %v", c.data, c.objectID.String(), result.String())
		}

		env.Repository.Blocks.Flush(ctx)
	}
}

func objectIDsEqual(o1 object.ID, o2 object.ID) bool {
	return reflect.DeepEqual(o1, o2)
}

func TestWriterCompleteChunkInTwoWrites(t *testing.T) {
	var env repotesting.Environment
	defer env.Setup(t).Close(t)
	ctx := context.Background()

	bytes := make([]byte, 100)
	writer := env.Repository.Objects.NewWriter(ctx, object.WriterOptions{})
	writer.Write(bytes[0:50]) //nolint:errcheck
	writer.Write(bytes[0:50]) //nolint:errcheck
	result, err := writer.Result()
	if result != "1d804f1f69df08f3f59070bf962de69433e3d61ac18522a805a84d8c92741340" {
		t.Errorf("unexpected result: %v err: %v", result, err)
	}
}

func TestPackingSimple(t *testing.T) {
	var env repotesting.Environment
	defer env.Setup(t).Close(t)

	ctx := context.Background()

	content1 := "hello, how do you do?"
	content2 := "hi, how are you?"
	content3 := "thank you!"

	oid1a := writeObject(ctx, t, env.Repository, []byte(content1), "packed-object-1a")
	oid1b := writeObject(ctx, t, env.Repository, []byte(content1), "packed-object-1b")
	oid2a := writeObject(ctx, t, env.Repository, []byte(content2), "packed-object-2a")
	oid2b := writeObject(ctx, t, env.Repository, []byte(content2), "packed-object-2b")

	oid3a := writeObject(ctx, t, env.Repository, []byte(content3), "packed-object-3a")
	oid3b := writeObject(ctx, t, env.Repository, []byte(content3), "packed-object-3b")
	verify(ctx, t, env.Repository, oid1a, []byte(content1), "packed-object-1")
	verify(ctx, t, env.Repository, oid2a, []byte(content2), "packed-object-2")
	oid2c := writeObject(ctx, t, env.Repository, []byte(content2), "packed-object-2c")
	oid1c := writeObject(ctx, t, env.Repository, []byte(content1), "packed-object-1c")

	env.Repository.Blocks.Flush(ctx)

	if got, want := oid1a.String(), oid1b.String(); got != want {
		t.Errorf("oid1a(%q) != oid1b(%q)", got, want)
	}
	if got, want := oid1a.String(), oid1c.String(); got != want {
		t.Errorf("oid1a(%q) != oid1c(%q)", got, want)
	}
	if got, want := oid2a.String(), oid2b.String(); got != want {
		t.Errorf("oid2(%q)a != oidb(%q)", got, want)
	}
	if got, want := oid2a.String(), oid2c.String(); got != want {
		t.Errorf("oid2(%q)a != oidc(%q)", got, want)
	}
	if got, want := oid3a.String(), oid3b.String(); got != want {
		t.Errorf("oid3a(%q) != oid3b(%q)", got, want)
	}

	env.VerifyStorageBlockCount(t, 3)

	env.MustReopen(t)

	verify(ctx, t, env.Repository, oid1a, []byte(content1), "packed-object-1")
	verify(ctx, t, env.Repository, oid2a, []byte(content2), "packed-object-2")
	verify(ctx, t, env.Repository, oid3a, []byte(content3), "packed-object-3")

	if err := env.Repository.Blocks.CompactIndexes(ctx, block.CompactOptions{MinSmallBlocks: 1, MaxSmallBlocks: 1}); err != nil {
		t.Errorf("optimize error: %v", err)
	}

	env.MustReopen(t)

	verify(ctx, t, env.Repository, oid1a, []byte(content1), "packed-object-1")
	verify(ctx, t, env.Repository, oid2a, []byte(content2), "packed-object-2")
	verify(ctx, t, env.Repository, oid3a, []byte(content3), "packed-object-3")

	if err := env.Repository.Blocks.CompactIndexes(ctx, block.CompactOptions{MinSmallBlocks: 1, MaxSmallBlocks: 1}); err != nil {
		t.Errorf("optimize error: %v", err)
	}

	env.MustReopen(t)

	verify(ctx, t, env.Repository, oid1a, []byte(content1), "packed-object-1")
	verify(ctx, t, env.Repository, oid2a, []byte(content2), "packed-object-2")
	verify(ctx, t, env.Repository, oid3a, []byte(content3), "packed-object-3")
}

func TestHMAC(t *testing.T) {
	var env repotesting.Environment
	defer env.Setup(t).Close(t)
	ctx := context.Background()

	content := bytes.Repeat([]byte{0xcd}, 50)

	w := env.Repository.Objects.NewWriter(ctx, object.WriterOptions{})
	w.Write(content) //nolint:errcheck
	result, err := w.Result()
	if result.String() != "367352007ee6ca9fa755ce8352347d092c17a24077fd33c62f655574a8cf906d" {
		t.Errorf("unexpected result: %v err: %v", result.String(), err)
	}
}

func TestUpgrade(t *testing.T) {
	var env repotesting.Environment
	defer env.Setup(t).Close(t)
	ctx := context.Background()

	if err := env.Repository.Upgrade(ctx); err != nil {
		t.Errorf("upgrade error: %v", err)
	}

	if err := env.Repository.Upgrade(ctx); err != nil {
		t.Errorf("2nd upgrade error: %v", err)
	}
}

func TestReaderStoredBlockNotFound(t *testing.T) {
	var env repotesting.Environment
	defer env.Setup(t).Close(t)
	ctx := context.Background()

	objectID, err := object.ParseID("Ddeadbeef")
	if err != nil {
		t.Errorf("cannot parse object ID: %v", err)
	}
	reader, err := env.Repository.Objects.Open(ctx, objectID)
	if err != storage.ErrBlockNotFound || reader != nil {
		t.Errorf("unexpected result: reader: %v err: %v", reader, err)
	}
}

func TestEndToEndReadAndSeek(t *testing.T) {
	var env repotesting.Environment
	defer env.Setup(t).Close(t)
	ctx := context.Background()

	for _, size := range []int{1, 199, 200, 201, 9999, 512434} {
		// Create some random data sample of the specified size.
		randomData := make([]byte, size)
		cryptorand.Read(randomData) //nolint:errcheck

		writer := env.Repository.Objects.NewWriter(ctx, object.WriterOptions{})
		writer.Write(randomData) //nolint:errcheck
		objectID, err := writer.Result()
		writer.Close()
		if err != nil {
			t.Errorf("cannot get writer result for %v: %v", size, err)
			continue
		}

		verify(ctx, t, env.Repository, objectID, randomData, fmt.Sprintf("%v %v", objectID, size))
	}
}

func writeObject(ctx context.Context, t *testing.T, rep *repo.Repository, data []byte, testCaseID string) object.ID {
	w := rep.Objects.NewWriter(ctx, object.WriterOptions{})
	if _, err := w.Write(data); err != nil {
		t.Fatalf("can't write object %q - write failed: %v", testCaseID, err)

	}
	oid, err := w.Result()
	if err != nil {
		t.Fatalf("can't write object %q - result failed: %v", testCaseID, err)
	}

	return oid
}

func verify(ctx context.Context, t *testing.T, rep *repo.Repository, objectID object.ID, expectedData []byte, testCaseID string) {
	t.Helper()
	reader, err := rep.Objects.Open(ctx, objectID)
	if err != nil {
		t.Errorf("cannot get reader for %v (%v): %v %v", testCaseID, objectID, err, string(debug.Stack()))
		return
	}

	for i := 0; i < 20; i++ {
		sampleSize := int(rand.Int31n(300))
		seekOffset := int(rand.Int31n(int32(len(expectedData))))
		if seekOffset+sampleSize > len(expectedData) {
			sampleSize = len(expectedData) - seekOffset
		}
		if sampleSize > 0 {
			got := make([]byte, sampleSize)
			if offset, err := reader.Seek(int64(seekOffset), 0); err != nil || offset != int64(seekOffset) {
				t.Errorf("seek error: %v offset=%v expected:%v", err, offset, seekOffset)
			}
			if n, err := reader.Read(got); err != nil || n != sampleSize {
				t.Errorf("invalid data: n=%v, expected=%v, err:%v", n, sampleSize, err)
			}

			expected := expectedData[seekOffset : seekOffset+sampleSize]

			if !bytes.Equal(expected, got) {
				t.Errorf("incorrect data read for %v: expected: %x, got: %x", testCaseID, expected, got)
			}
		}
	}
}

func TestFormats(t *testing.T) {
	ctx := context.Background()
	makeFormat := func(hash, encryption string) func(*repo.NewRepositoryOptions) {
		return func(n *repo.NewRepositoryOptions) {
			n.BlockFormat.Hash = hash
			n.BlockFormat.Encryption = encryption
			n.BlockFormat.HMACSecret = []byte("key")
			n.ObjectFormat.MaxBlockSize = 10000
			n.ObjectFormat.Splitter = "FIXED"
		}
	}

	cases := []struct {
		format func(*repo.NewRepositoryOptions)
		oids   map[string]object.ID
	}{
		{
			format: func(n *repo.NewRepositoryOptions) {
				n.ObjectFormat.MaxBlockSize = 10000
			},
			oids: map[string]object.ID{
				"": "b613679a0814d9ec772f95d778c35fc5ff1697c493715653c6c712144292c5ad",
				"The quick brown fox jumps over the lazy dog": "fb011e6154a19b9a4c767373c305275a5a69e8b68b0b4c9200c383dced19a416",
			},
		},
		{
			format: makeFormat("HMAC-SHA256", "NONE"),
			oids: map[string]object.ID{
				"The quick brown fox jumps over the lazy dog": "f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8",
			},
		},
		{
			format: makeFormat("HMAC-SHA256-128", "NONE"),
			oids: map[string]object.ID{
				"The quick brown fox jumps over the lazy dog": "f7bc83f430538424b13298e6aa6fb143",
			},
		},
	}

	for caseIndex, c := range cases {
		var env repotesting.Environment
		defer env.Setup(t, c.format).Close(t)

		for k, v := range c.oids {
			bytesToWrite := []byte(k)
			w := env.Repository.Objects.NewWriter(ctx, object.WriterOptions{})
			w.Write(bytesToWrite) //nolint:errcheck
			oid, err := w.Result()
			if err != nil {
				t.Errorf("error: %v", err)
			}
			if !objectIDsEqual(oid, v) {
				t.Errorf("invalid oid for #%v\ngot:\n%#v\nexpected:\n%#v", caseIndex, oid.String(), v.String())
			}

			rc, err := env.Repository.Objects.Open(ctx, oid)
			if err != nil {
				t.Errorf("open failed: %v", err)
				continue
			}
			bytesRead, err := ioutil.ReadAll(rc)
			if err != nil {
				t.Errorf("error reading: %v", err)
			}
			if !bytes.Equal(bytesRead, bytesToWrite) {
				t.Errorf("data mismatch, read:%x vs written:%v", bytesRead, bytesToWrite)
			}
		}
	}
}
