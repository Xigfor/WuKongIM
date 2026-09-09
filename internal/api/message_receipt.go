package api

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/WuKongIM/WuKongIM/internal/options"
	"github.com/WuKongIM/WuKongIM/internal/service"
	"github.com/WuKongIM/WuKongIM/internal/types"
	"github.com/WuKongIM/WuKongIM/pkg/wkdb"
	"github.com/WuKongIM/WuKongIM/pkg/wkhttp"
	"github.com/WuKongIM/WuKongIM/pkg/wkutil"
	wkproto "github.com/WuKongIM/WuKongIMGoProto"
)

const receiptBatchLimit = 200

type receiptRequest struct {
	UID          string   `json:"uid"`
	ChannelID    string   `json:"channel_id"`
	ChannelType  uint8    `json:"channel_type"`
	MessageSeqs  []uint32 `json:"message_seqs"`
	ExtraVersion uint64   `json:"extra_version"`
	Limit        int      `json:"limit"`
}

// Functions keep the HTTP boundary testable without a live cluster.
type messageReceiptAPI struct {
	authorize func(string, string) (bool, error)
	forward   func(*wkhttp.Context, []byte, string, bool) (bool, error)
	load      func(string, uint8, uint64) (wkdb.Message, error)
	record    func(string, []wkdb.MessageReceipt) error
	sync      func(string, uint64, int) ([]wkdb.MessageReceipt, error)
	notify    func(string, string) error
}

func newMessageReceiptAPI() *messageReceiptAPI {
	return &messageReceiptAPI{
		authorize: authorizeReceipt,
		forward:   forwardReceipt,
		load:      func(c string, t uint8, seq uint64) (wkdb.Message, error) { return service.Store.LoadMsg(c, t, seq) },
		record:    func(c string, r []wkdb.MessageReceipt) error { return service.Store.RecordMessageReceipts(c, r) },
		sync: func(c string, v uint64, l int) ([]wkdb.MessageReceipt, error) {
			return service.Store.SyncMessageReceipts(c, v, l)
		},
		notify: notifyMessageReaded,
	}
}

func (a *messageReceiptAPI) route(r *wkhttp.WKHttp) {
	r.POST("/message/receipt", a.receipt)
	r.POST("/message/extra/sync", a.syncExtra)
	// Verifies the registered IM device token on the user's slot leader.
	r.POST("/message/receipt/auth", receiptAuth)
}

func receiptError(c *wkhttp.Context, status int, message string) {
	c.JSON(status, map[string]interface{}{"status": status, "msg": message})
}

func (a *messageReceiptAPI) bind(c *wkhttp.Context, req *receiptRequest, messageLeader bool) (string, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32*1024)
	body, err := BindJSON(req, c)
	if err != nil || req.UID == "" || req.ChannelID == "" || req.UID == req.ChannelID ||
		req.ChannelType != wkproto.ChannelTypePerson || options.IsSpecialChar(req.UID) ||
		options.IsSpecialChar(req.ChannelID) || strings.TrimSpace(req.UID) != req.UID || strings.TrimSpace(req.ChannelID) != req.ChannelID {
		receiptError(c, http.StatusBadRequest, "valid uid and peer channel_id with channel_type=1 required")
		return "", false
	}
	token := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if token == "" {
		receiptError(c, http.StatusUnauthorized, "login required")
		return "", false
	}
	valid, err := a.authorize(req.UID, token)
	if err != nil {
		receiptError(c, http.StatusServiceUnavailable, "authentication unavailable")
		return "", false
	}
	if !valid {
		receiptError(c, http.StatusUnauthorized, "invalid user token")
		return "", false
	}
	channel := options.GetFakeChannelIDWith(req.UID, req.ChannelID)
	forwarded, err := a.forward(c, body, channel, messageLeader)
	if err != nil {
		receiptError(c, http.StatusServiceUnavailable, "channel unavailable")
		return "", false
	}
	return channel, !forwarded
}

func (a *messageReceiptAPI) receipt(c *wkhttp.Context) {
	var req receiptRequest
	channel, ok := a.bind(c, &req, true)
	if !ok {
		return
	}
	if len(req.MessageSeqs) == 0 || len(req.MessageSeqs) > receiptBatchLimit {
		receiptError(c, http.StatusBadRequest, "message_seqs must contain 1 to 200 items")
		return
	}
	receipts := make([]wkdb.MessageReceipt, 0, len(req.MessageSeqs))
	seen := make(map[uint32]bool)
	for _, seq := range req.MessageSeqs {
		if seq == 0 {
			receiptError(c, http.StatusBadRequest, "message sequence must be positive")
			return
		}
		if seen[seq] {
			continue
		}
		seen[seq] = true
		msg, err := a.load(channel, req.ChannelType, uint64(seq))
		if errors.Is(err, wkdb.ErrNotFound) || (err == nil && msg.MessageID == 0) {
			receiptError(c, http.StatusNotFound, "message not found in this conversation")
			return
		}
		if err != nil {
			receiptError(c, http.StatusServiceUnavailable, "message lookup failed")
			return
		}
		if msg.FromUID != req.ChannelID || msg.ChannelType != req.ChannelType {
			receiptError(c, http.StatusForbidden, "only received messages can be marked read")
			return
		}
		if msg.Setting&wkproto.SettingReceiptEnabled == 0 {
			receiptError(c, http.StatusBadRequest, "message does not request a receipt")
			return
		}
		receipts = append(receipts, wkdb.MessageReceipt{MessageID: strconv.FormatInt(msg.MessageID, 10), MessageSeq: seq, Reader: req.UID})
	}
	// Validate the entire batch before persisting any receipt.
	if err := a.record(channel, receipts); err != nil {
		receiptError(c, http.StatusServiceUnavailable, "receipt storage failed")
		return
	}
	// Duplicate retries resend the invalidation; the durable receipts remain unchanged.
	if err := a.notify(req.UID, req.ChannelID); err != nil {
		receiptError(c, http.StatusServiceUnavailable, "receipt notification failed; retry")
		return
	}
	c.ResponseOK()
}

func (a *messageReceiptAPI) syncExtra(c *wkhttp.Context) {
	var req receiptRequest
	channel, ok := a.bind(c, &req, false)
	if !ok {
		return
	}
	if req.Limit == 0 {
		req.Limit = 100
	}
	if req.Limit < 1 || req.Limit > receiptBatchLimit {
		receiptError(c, http.StatusBadRequest, "limit must be 1 to 200")
		return
	}
	items, err := a.sync(channel, req.ExtraVersion, req.Limit)
	if err != nil {
		receiptError(c, http.StatusServiceUnavailable, "receipt sync failed")
		return
	}
	if items == nil {
		items = []wkdb.MessageReceipt{}
	}
	c.JSON(http.StatusOK, items)
}

func forwardReceipt(c *wkhttp.Context, body []byte, channel string, messageLeader bool) (bool, error) {
	leader, err := service.Cluster.SlotLeaderOfChannel(channel, wkproto.ChannelTypePerson)
	if messageLeader {
		leader, err = service.Cluster.LeaderOfChannel(channel, wkproto.ChannelTypePerson)
	}
	if err != nil {
		return false, err
	}
	if options.G.IsLocalNode(leader.Id) {
		return false, nil
	}
	c.ForwardWithBody(strings.TrimRight(leader.ApiServerAddr, "/")+c.Request.URL.Path, body)
	return true, nil
}

var receiptHTTPClient = &http.Client{Timeout: 5 * time.Second}

func authorizeReceipt(uid, token string) (bool, error) {
	leader, err := service.Cluster.SlotLeaderOfChannel(uid, wkproto.ChannelTypePerson)
	if err != nil {
		return false, err
	}
	if options.G.IsLocalNode(leader.Id) {
		return localReceiptAuth(uid, token)
	}
	body, _ := json.Marshal(map[string]string{"uid": uid})
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(leader.ApiServerAddr, "/")+"/message/receipt/auth", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if options.G.ManagerToken != "" {
		req.Header.Set("token", options.G.ManagerToken)
	}
	resp, err := receiptHTTPClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusUnauthorized {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("receipt authentication status %d", resp.StatusCode)
	}
	return true, nil
}

func localReceiptAuth(uid, token string) (bool, error) {
	if uid == "" || token == "" {
		return false, nil
	}
	devices, err := service.Store.DB().GetDevices(uid)
	if errors.Is(err, wkdb.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, device := range devices {
		if device.Token != "" && subtle.ConstantTimeCompare([]byte(device.Token), []byte(token)) == 1 {
			return true, nil
		}
	}
	return false, nil
}

func receiptAuth(c *wkhttp.Context) {
	var req struct {
		UID string `json:"uid"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	body, err := BindJSON(&req, c)
	if err != nil || req.UID == "" {
		receiptError(c, 400, "uid required")
		return
	}
	forwarded, err := forwardReceipt(c, body, req.UID, false)
	if err != nil {
		receiptError(c, 503, "authentication unavailable")
		return
	}
	if forwarded {
		return
	}
	valid, err := localReceiptAuth(req.UID, strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	if err != nil {
		receiptError(c, 503, "authentication unavailable")
		return
	}
	if !valid {
		receiptError(c, 401, "invalid user token")
		return
	}
	c.ResponseOK()
}

func notifyMessageReaded(reader, peer string) error {
	payload, _ := json.Marshal(map[string]interface{}{"type": 99, "cmd": "messageReaded", "param": map[string]interface{}{"channel_id": reader, "channel_type": 1}})
	req := messageSendReq{FromUID: reader, ChannelID: peer, ChannelType: 1, Header: types.MessageHeader{SyncOnce: 1}, Payload: payload}
	_, err := sendMessageToChannel(req, peer, 1, wkutil.GenUUID(), wkproto.StreamFlagIng)
	return err
}
