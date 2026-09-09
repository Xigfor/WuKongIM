package store

import (
	"github.com/WuKongIM/WuKongIM/pkg/cluster/icluster"
	"github.com/WuKongIM/WuKongIM/pkg/raft/types"
	"github.com/WuKongIM/WuKongIM/pkg/wkdb"
	"github.com/stretchr/testify/require"
	"testing"
)

type receiptTestSlot struct {
	icluster.Slot
	apply func(uint32, []byte) (*types.ProposeResp, error)
}

func (s receiptTestSlot) GetSlotId(string) uint32 { return 3 }
func (s receiptTestSlot) ProposeUntilApplied(slot uint32, data []byte) (*types.ProposeResp, error) {
	return s.apply(slot, data)
}

func TestMessageReceiptSlotCommandApplyAndReplay(t *testing.T) {
	db := wkdb.NewWukongDB(wkdb.NewOptions(wkdb.WithDir(t.TempDir()), wkdb.WithShardNum(1)))
	require.NoError(t, db.Open())
	defer db.Close()
	s := New(NewOptions(WithDB(db)))
	var encoded []byte
	s.opts.Slot = receiptTestSlot{apply: func(slot uint32, data []byte) (*types.ProposeResp, error) {
		require.Equal(t, uint32(3), slot)
		encoded = data
		return &types.ProposeResp{}, s.ApplySlotLogs(slot, []types.Log{{Index: 1, Data: data}})
	}}
	require.NoError(t, s.RecordMessageReceipts("reader@sender", []wkdb.MessageReceipt{{MessageSeq: 101, MessageID: "message-101", Reader: "reader"}}))
	// Replaying a replicated log never adds a second read or advances its cursor.
	require.NoError(t, s.ApplySlotLogs(3, []types.Log{{Index: 1, Data: encoded}}))
	page, err := s.SyncMessageReceipts("reader@sender", 0, 100)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, uint64(1), page[0].ExtraVersion)
}
