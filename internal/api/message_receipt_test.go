package api

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WuKongIM/WuKongIM/internal/options"
	"github.com/WuKongIM/WuKongIM/pkg/wkdb"
	"github.com/WuKongIM/WuKongIM/pkg/wkhttp"
	wkproto "github.com/WuKongIM/WuKongIMGoProto"
	"github.com/stretchr/testify/require"
)

func TestMessageReceiptHTTPContract(t *testing.T) {
	for _, tc := range []struct {
		name, body, token string
		want              int
	}{
		{"client JSON", `{"uid":"42","channel_id":"99","channel_type":1,"message_seqs":[101,102,101]}`, "valid", 200},
		{"no token", `{"uid":"42","channel_id":"99","channel_type":1,"message_seqs":[101]}`, "", 401},
		{"spoof uid", `{"uid":"43","channel_id":"99","channel_type":1,"message_seqs":[101]}`, "valid", 401},
		{"own message", `{"uid":"42","channel_id":"99","channel_type":1,"message_seqs":[103]}`, "valid", 403},
		{"mixed invalid atomic", `{"uid":"42","channel_id":"99","channel_type":1,"message_seqs":[101,999]}`, "valid", 404},
		{"zero", `{"uid":"42","channel_id":"99","channel_type":1,"message_seqs":[0]}`, "valid", 400},
		{"negative", `{"uid":"42","channel_id":"99","channel_type":1,"message_seqs":[-1]}`, "valid", 400},
		{"overflow", `{"uid":"42","channel_id":"99","channel_type":1,"message_seqs":[4294967296]}`, "valid", 400},
		{"group", `{"uid":"42","channel_id":"99","channel_type":2,"message_seqs":[101]}`, "valid", 400},
		{"empty", `{"uid":"42","channel_id":"99","channel_type":1,"message_seqs":[]}`, "valid", 400},
		{"no receipt bit", `{"uid":"42","channel_id":"99","channel_type":1,"message_seqs":[104]}`, "valid", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stored []wkdb.MessageReceipt
			var notified bool
			a := &messageReceiptAPI{
				authorize: func(uid, token string) (bool, error) { return uid == "42" && token == "valid", nil },
				forward:   func(*wkhttp.Context, []byte, string, bool) (bool, error) { return false, nil },
				load: func(ch string, typ uint8, seq uint64) (wkdb.Message, error) {
					require.Equal(t, options.GetFakeChannelIDWith("42", "99"), ch)
					if seq == 999 {
						return wkdb.Message{}, wkdb.ErrNotFound
					}
					from := "99"
					if seq == 103 {
						from = "42"
					}
					setting := wkproto.SettingReceiptEnabled
					if seq == 104 {
						setting = 0
					}
					return wkdb.Message{RecvPacket: wkproto.RecvPacket{MessageID: int64(seq) + 9007199254740992, MessageSeq: uint32(seq), FromUID: from, ChannelType: typ, Setting: setting}}, nil
				},
				record: func(ch string, r []wkdb.MessageReceipt) error { stored = r; return nil },
				notify: func(reader, peer string) error {
					require.Equal(t, "42", reader)
					require.Equal(t, "99", peer)
					notified = true
					return nil
				},
			}
			r := wkhttp.New()
			a.route(r)
			req := httptest.NewRequest("POST", "/message/receipt", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			resp := httptest.NewRecorder()
			r.ServeHTTP(resp, req)
			require.Equal(t, tc.want, resp.Code, resp.Body.String())
			if tc.want == 200 {
				require.Len(t, stored, 2)
				require.True(t, notified)
				require.Equal(t, "9007199254741093", stored[0].MessageID)
			} else {
				require.Empty(t, stored)
				require.False(t, notified)
			}
		})
	}
}

func TestMessageReceiptSyncArrayAndFailures(t *testing.T) {
	a := &messageReceiptAPI{
		authorize: func(string, string) (bool, error) { return true, nil },
		forward:   func(*wkhttp.Context, []byte, string, bool) (bool, error) { return false, nil },
		sync: func(ch string, version uint64, limit int) ([]wkdb.MessageReceipt, error) {
			require.Equal(t, uint64(12), version)
			require.Equal(t, 100, limit)
			return []wkdb.MessageReceipt{{MessageID: "9007199254740993", ExtraVersion: 13, Readed: 1, ReadedCount: 1}}, nil
		},
	}
	r := wkhttp.New()
	a.route(r)
	body := `{"uid":"42","channel_id":"99","channel_type":1,"source":"42","limit":100,"extra_version":12}`
	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/message/extra/sync", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer valid")
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		return out
	}
	resp := call()
	require.Equal(t, 200, resp.Code)
	var page []wkdb.MessageReceipt
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page))
	require.Len(t, page, 1)
	require.Equal(t, 1, page[0].Readed)
	a.sync = func(string, uint64, int) ([]wkdb.MessageReceipt, error) { return nil, errors.New("db down") }
	require.Equal(t, 503, call().Code)
}
