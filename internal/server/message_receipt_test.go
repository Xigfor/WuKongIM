//go:build receipt_integration

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WuKongIM/WuKongIM/internal/options"
	"github.com/WuKongIM/WuKongIM/pkg/jsonrpc"
	"github.com/WuKongIM/WuKongIM/pkg/wkdb"
	wkproto "github.com/WuKongIM/WuKongIMGoProto"
	"github.com/stretchr/testify/require"
)

type receiptMallTestTransport func(*http.Request) (*http.Response, error)

func (f receiptMallTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A real isolated node exercises TCP send -> JSON receipt -> durable slot apply
// -> sender command -> incremental sync, including authenticated failure paths.
func TestMessageReceiptRoundTrip(t *testing.T) {
	// Isolated node test: stub only the external mall session authority, retaining
	// the real TCP, HTTP, slot persistence, and notification paths.
	originalTransport := http.DefaultTransport
	http.DefaultTransport = receiptMallTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://mall-portal.xigfor.com/sso/info" {
			return originalTransport.RoundTrip(r)
		}
		uid := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer test-")
		body := `{"code":401}`
		if uid == "900000000001" || uid == "900000000002" {
			body = `{"code":200,"data":{"id":` + uid + `}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	defer func() { http.DefaultTransport = originalTransport }()

	addr := func() string {
		l, e := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, e)
		a := l.Addr().String()
		l.Close()
		return a
	}
	tcp, httpAddr, ws, clusterAddr := addr(), addr(), addr(), addr()
	s := NewTestServer(t, options.WithAddr("tcp://"+tcp), options.WithHTTPAddr(httpAddr),
		options.WithWSAddr("ws://"+ws), options.WithClusterAddr("tcp://"+clusterAddr),
		options.WithClusterAPIURL("http://"+httpAddr), options.WithExternalTCPAddr(tcp),
		options.WithDemoOn(false), options.WithManagerOn(false), options.WithTokenAuthOn(true),
		options.WithClusterInitNodes([]*options.Node{{Id: 1001, ServerAddr: clusterAddr}}),
		func(o *options.Options) { o.Plugin.SocketPath = filepath.Join(t.TempDir(), "wk.sock") })
	s.opts.Mode = options.TestMode
	require.NoError(t, s.Start())
	defer s.StopNoErr()
	s.MustWaitAllSlotsReady(10 * time.Second)
	hc := &http.Client{Timeout: 8 * time.Second}
	post := func(path, token string, body interface{}, want int) []byte {
		data, e := json.Marshal(body)
		require.NoError(t, e)
		req, e := http.NewRequest("POST", "http://"+httpAddr+path, bytes.NewReader(data))
		require.NoError(t, e)
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, e := hc.Do(req)
		require.NoError(t, e)
		defer resp.Body.Close()
		out, e := io.ReadAll(resp.Body)
		require.NoError(t, e)
		require.Equal(t, want, resp.StatusCode, string(out))
		return out
	}
	for _, uid := range []string{"900000000001", "900000000002"} {
		post("/user/token", "", map[string]interface{}{"uid": uid, "token": "test-" + uid, "device_flag": 0, "device_level": 1}, 200)
	}
	connect := func(uid string) net.Conn {
		conn := connectRawTCP(t, tcp)
		sendJSON(t, conn, jsonrpc.ConnectRequest{BaseRequest: jsonrpc.BaseRequest{Method: jsonrpc.MethodConnect, ID: uid}, Params: jsonrpc.ConnectParams{UID: uid, Token: "test-" + uid, DeviceID: uid, Version: wkproto.LatestVersion}})
		msg, _ := readJSON(t, conn, 5*time.Second)
		response := assertDecodedAs[jsonrpc.GenericResponse](t, msg)
		require.Nil(t, response.Error)
		return conn
	}
	sender, reader := connect("900000000001"), connect("900000000002")
	defer sender.Close()
	defer reader.Close()
	sendJSON(t, sender, jsonrpc.SendRequest{BaseRequest: jsonrpc.BaseRequest{Method: jsonrpc.MethodSend, ID: "receipt-send"}, Params: jsonrpc.SendParams{
		ChannelID: "900000000002", ChannelType: 1, Setting: jsonrpc.SettingFlags{Receipt: true}, Payload: json.RawMessage(`{"type":1,"content":"receipt test"}`)}})
	sent, _ := readJSON(t, sender, 8*time.Second)
	require.Nil(t, assertDecodedAs[jsonrpc.GenericResponse](t, sent).Error)
	received, _ := readJSON(t, reader, 8*time.Second)
	msg := assertDecodedAs[jsonrpc.RecvNotification](t, received)
	require.True(t, msg.Params.Setting.Receipt)
	body := map[string]interface{}{"uid": "900000000002", "channel_id": "900000000001", "channel_type": 1, "message_seqs": []uint32{msg.Params.MessageSeq}}
	post("/message/receipt", "test-900000000001", body, 401)
	post("/message/receipt", "", body, 401)
	post("/message/receipt", "test-900000000002", body, 200)
	notification, _ := readJSON(t, sender, 8*time.Second)
	notice := assertDecodedAs[jsonrpc.RecvNotification](t, notification)
	var command struct {
		Type  int    `json:"type"`
		Cmd   string `json:"cmd"`
		Param struct {
			ChannelID   string `json:"channel_id"`
			ChannelType int    `json:"channel_type"`
		} `json:"param"`
	}
	require.NoError(t, json.Unmarshal(notice.Params.Payload, &command))
	require.Equal(t, 99, command.Type)
	require.Equal(t, "messageReaded", command.Cmd)
	require.Equal(t, "900000000002", command.Param.ChannelID)
	syncBody := map[string]interface{}{"uid": "900000000001", "channel_id": "900000000002", "channel_type": 1, "extra_version": 0, "limit": 100}
	var page []wkdb.MessageReceipt
	require.NoError(t, json.Unmarshal(post("/message/extra/sync", "test-900000000001", syncBody, 200), &page))
	require.Len(t, page, 1)
	require.Equal(t, msg.Params.MessageID, page[0].MessageID)
	require.Equal(t, 1, page[0].ReadedCount)
	post("/message/receipt", "test-900000000002", body, 200)
	syncBody["extra_version"] = page[0].ExtraVersion
	require.JSONEq(t, `[]`, string(post("/message/extra/sync", "test-900000000001", syncBody, 200)))
}
