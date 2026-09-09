package wkdb

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble"
)

// MessageReceipt is immutable after the first read. ExtraVersion is a per-channel
// commit-order cursor, not the message sequence (older messages may be read later).
type MessageReceipt struct {
	MessageID    string `json:"message_id_str"`
	MessageSeq   uint32 `json:"message_seq"`
	Reader       string `json:"reader"`
	Readed       int    `json:"readed"`
	ReadedCount  int    `json:"readed_count"`
	ExtraVersion uint64 `json:"extra_version"`
}

type MessageReceiptDB interface {
	RecordMessageReceipts(channel string, receipts []MessageReceipt) error
	SyncMessageReceipts(channel string, after uint64, limit int) ([]MessageReceipt, error)
}

// Dedicated 0x1801 namespace; length-prefixed channel IDs avoid ambiguous keys.
func receiptKey(channel string, kind byte, number uint64) []byte {
	b := make([]byte, 7+len(channel)+8)
	b[0], b[1] = 0x18, 0x01
	binary.BigEndian.PutUint32(b[2:6], uint32(len(channel)))
	copy(b[6:], channel)
	b[6+len(channel)] = kind
	binary.BigEndian.PutUint64(b[len(b)-8:], number)
	return b
}

func (wk *wukongDB) RecordMessageReceipts(channel string, receipts []MessageReceipt) error {
	wk.receiptMu.Lock()
	defer wk.receiptMu.Unlock()
	db := wk.shardDB(channel)
	var version uint64
	v, closer, err := db.Get(receiptKey(channel, 0, 0))
	if err == nil {
		if len(v) != 8 {
			closer.Close()
			return fmt.Errorf("invalid receipt cursor")
		}
		version = binary.BigEndian.Uint64(v)
		closer.Close()
	} else if !errors.Is(err, pebble.ErrNotFound) {
		return err
	}
	batch := db.NewBatch()
	defer batch.Close()
	seen := make(map[uint32]bool)
	for _, receipt := range receipts {
		if seen[receipt.MessageSeq] {
			continue
		}
		seen[receipt.MessageSeq] = true
		msgKey := receiptKey(channel, 1, uint64(receipt.MessageSeq))
		_, closer, err := db.Get(msgKey)
		if err == nil {
			closer.Close()
			continue
		}
		if !errors.Is(err, pebble.ErrNotFound) {
			return err
		}
		version++
		receipt.ExtraVersion, receipt.Readed, receipt.ReadedCount = version, 1, 1
		data, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		if err = batch.Set(msgKey, data, nil); err != nil {
			return err
		}
		if err = batch.Set(receiptKey(channel, 2, version), data, nil); err != nil {
			return err
		}
	}
	var cursor [8]byte
	binary.BigEndian.PutUint64(cursor[:], version)
	if err = batch.Set(receiptKey(channel, 0, 0), cursor[:], nil); err != nil {
		return err
	}
	return batch.Commit(pebble.Sync)
}

func (wk *wukongDB) SyncMessageReceipts(channel string, after uint64, limit int) ([]MessageReceipt, error) {
	result := make([]MessageReceipt, 0)
	if limit <= 0 || after == ^uint64(0) {
		return result, nil
	}
	iter := wk.shardDB(channel).NewIter(&pebble.IterOptions{
		LowerBound: receiptKey(channel, 2, after+1),
		UpperBound: receiptKey(channel, 3, 0),
	})
	defer iter.Close()
	for iter.First(); iter.Valid() && len(result) < limit; iter.Next() {
		var receipt MessageReceipt
		if err := json.Unmarshal(iter.Value(), &receipt); err != nil {
			return nil, err
		}
		result = append(result, receipt)
	}
	return result, iter.Error()
}
