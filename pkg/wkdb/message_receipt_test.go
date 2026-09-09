package wkdb_test

import (
	"fmt"
	"github.com/WuKongIM/WuKongIM/pkg/wkdb"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
)

func TestMessageReceiptsDurableOrderedAndIsolated(t *testing.T) {
	d := newTestDB(t)
	require.NoError(t, d.Open())
	defer d.Close()
	r := func(seq uint32) wkdb.MessageReceipt {
		return wkdb.MessageReceipt{MessageSeq: seq, MessageID: fmt.Sprint(seq), Reader: "reader"}
	}
	require.NoError(t, d.RecordMessageReceipts("a@b", []wkdb.MessageReceipt{r(102), r(102)}))
	require.NoError(t, d.RecordMessageReceipts("a@b", []wkdb.MessageReceipt{r(101)}))
	require.NoError(t, d.RecordMessageReceipts("x@y", []wkdb.MessageReceipt{r(999)}))
	page, err := d.SyncMessageReceipts("a@b", 0, 1)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, uint32(102), page[0].MessageSeq)
	require.Equal(t, uint64(1), page[0].ExtraVersion)
	require.Equal(t, 1, page[0].ReadedCount)
	page, err = d.SyncMessageReceipts("a@b", 1, 100)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, uint32(101), page[0].MessageSeq)
	require.Equal(t, uint64(2), page[0].ExtraVersion)
	require.NoError(t, d.RecordMessageReceipts("a@b", []wkdb.MessageReceipt{r(102)}))
	page, err = d.SyncMessageReceipts("a@b", 2, 100)
	require.NoError(t, err)
	require.Empty(t, page)
}

func TestMessageReceiptsSurviveReopenAndConcurrentRetries(t *testing.T) {
	dir := t.TempDir()
	opts := wkdb.NewOptions(wkdb.WithDir(dir), wkdb.WithShardNum(1))
	d := wkdb.NewWukongDB(opts)
	require.NoError(t, d.Open())
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- d.RecordMessageReceipts("a@b", []wkdb.MessageReceipt{{MessageSeq: 1, MessageID: "9007199254740993", Reader: "b"}})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.NoError(t, d.Close())
	d = wkdb.NewWukongDB(opts)
	require.NoError(t, d.Open())
	defer d.Close()
	page, err := d.SyncMessageReceipts("a@b", 0, 100)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, "9007199254740993", page[0].MessageID)
	require.Equal(t, uint64(1), page[0].ExtraVersion)
}
