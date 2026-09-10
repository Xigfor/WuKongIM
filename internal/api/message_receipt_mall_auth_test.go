package api

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WuKongIM/WuKongIM/pkg/wkdb"
	"github.com/WuKongIM/WuKongIM/pkg/wkhttp"
	wkproto "github.com/WuKongIM/WuKongIMGoProto"
	"github.com/stretchr/testify/require"
)

type receiptAuthTransport func(*http.Request) (*http.Response, error)

func (f receiptAuthTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise the production authorizer through BOTH App routes, using the mall
// login Bearer header and actual /sso/info envelope. No IM device registration.
func TestMessageReceiptMallLoginBoundary(t *testing.T) {
	for _, path := range []string{"/message/receipt", "/message/extra/sync"} {
		for _, tc := range []struct {
			name, header, response string
			status, want           int
			unavailable            bool
		}{
			{"mall session", "Bearer mall-session", `{"code":200,"data":{"id":9007199254740993}}`, 200, 200, false},
			{"string member ID", "Bearer mall-session", `{"code":200,"data":{"id":"9007199254740993"}}`, 200, 200, false},
			{"spoofed uid", "Bearer mall-session", `{"code":200,"data":{"id":42}}`, 200, 401, false},
			{"expired token", "Bearer expired", `{"code":401}`, 200, 401, false},
			{"IM device token is not mall session", "Bearer im-device", `{"code":401}`, 200, 401, false},
			{"HTTP unauthorized", "Bearer invalid", `{}`, 401, 401, false},
			{"no header", "", `{}`, 200, 401, false},
			{"raw token", "mall-session", `{}`, 200, 401, false},
			{"other scheme", "Basic mall-session", `{}`, 200, 401, false},
			{"missing identity", "Bearer mall-session", `{"code":200,"data":{}}`, 200, 503, false},
			{"bad response", "Bearer mall-session", `not JSON`, 200, 503, false},
			{"upstream error", "Bearer mall-session", `{"code":500}`, 200, 503, false},
			{"upstream redirect", "Bearer mall-session", `{}`, 302, 503, false},
			{"upstream down", "Bearer mall-session", `{}`, 503, 503, false},
			{"network unavailable", "Bearer mall-session", `{}`, 200, 503, true},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				previous := receiptHTTPClient
				calls := 0
				receiptHTTPClient = &http.Client{Transport: receiptAuthTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					require.Equal(t, receiptMemberInfoURL, r.URL.String())
					require.Equal(t, "GET", r.Method)
					require.Equal(t, tc.header, r.Header.Get("Authorization"))
					if tc.unavailable {
						return nil, errors.New("private upstream detail")
					}
					return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.response)), Header: make(http.Header)}, nil
				})}
				defer func() { receiptHTTPClient = previous }()
				a := newMessageReceiptAPI()
				reached := false
				a.forward = func(*wkhttp.Context, []byte, string, bool) (bool, error) { reached = true; return false, nil }
				a.load = func(_ string, _ uint8, seq uint64) (wkdb.Message, error) {
					return wkdb.Message{RecvPacket: wkproto.RecvPacket{MessageID: 101, MessageSeq: uint32(seq), FromUID: "99", ChannelType: 1, Setting: wkproto.SettingReceiptEnabled}}, nil
				}
				a.record = func(string, []wkdb.MessageReceipt) error { return nil }
				a.notify = func(string, string) error { return nil }
				a.sync = func(string, uint64, int) ([]wkdb.MessageReceipt, error) { return []wkdb.MessageReceipt{}, nil }
				router := wkhttp.New()
				a.route(router)
				req := httptest.NewRequest("POST", path, strings.NewReader(`{"uid":"9007199254740993","channel_id":"99","channel_type":1,"message_seqs":[101,102],"extra_version":0,"limit":100}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", tc.header)
				out := httptest.NewRecorder()
				router.ServeHTTP(out, req)
				require.Equal(t, tc.want, out.Code, out.Body.String())
				require.Equal(t, tc.want == 200, reached)
				require.NotContains(t, out.Body.String(), "mall-session")
				require.NotContains(t, out.Body.String(), "private upstream detail")
				if tc.header == "" || !strings.HasPrefix(tc.header, "Bearer ") {
					require.Zero(t, calls)
				} else {
					require.Equal(t, 1, calls)
				}
			})
		}
	}
}

func TestMessageReceiptMallAuthRejectsRedirect(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer origin.Close()
	resp, err := receiptHTTPClient.Get(origin.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusFound, resp.StatusCode)
	require.False(t, called)
}
