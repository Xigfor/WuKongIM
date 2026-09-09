package store

import (
	"encoding/json"
	"github.com/WuKongIM/WuKongIM/pkg/wkdb"
)

type messageReceiptsCommand struct {
	Channel  string                `json:"channel"`
	Receipts []wkdb.MessageReceipt `json:"receipts"`
}

func (s *Store) RecordMessageReceipts(channel string, receipts []wkdb.MessageReceipt) error {
	data, err := json.Marshal(messageReceiptsCommand{Channel: channel, Receipts: receipts})
	if err != nil {
		return err
	}
	data, err = NewCMD(CMDRecordMessageReceipts, data).Marshal()
	if err != nil {
		return err
	}
	_, err = s.opts.Slot.ProposeUntilApplied(s.opts.Slot.GetSlotId(channel), data)
	return err
}

func (s *Store) applyMessageReceipts(cmd *CMD) error {
	var request messageReceiptsCommand
	if err := json.Unmarshal(cmd.Data, &request); err != nil {
		return err
	}
	return s.wdb.RecordMessageReceipts(request.Channel, request.Receipts)
}

func (s *Store) SyncMessageReceipts(channel string, after uint64, limit int) ([]wkdb.MessageReceipt, error) {
	return s.wdb.SyncMessageReceipts(channel, after, limit)
}
