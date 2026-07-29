package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WuKongIM/WuKongIM/pkg/wkhttp"
	"github.com/stretchr/testify/require"
)

func TestChannelMessageSyncRoutesAreRegistered(t *testing.T) {
	router := wkhttp.New()
	newMessage(nil).route(router)

	for _, path := range []string{"/message/channel/sync", "/channel/messagesync"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost,
				path,
				strings.NewReader(`{"uid":"member-42"}`),
			)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			require.NotEqual(t, http.StatusNotFound, response.Code)
		})
	}
}

func TestSyncMessagesRequestAcceptsLegacyClientUID(t *testing.T) {
	var request syncMessagesReq
	err := json.Unmarshal(
		[]byte(`{"uid":"member-42","channel_id":"member-99","channel_type":1,`+
			`"start_message_seq":10,"end_message_seq":20,"limit":30,"pull_mode":1}`),
		&request,
	)
	require.NoError(t, err)

	request.normalizeLoginUID()

	require.Equal(t, "member-42", request.LoginUID)
	require.Equal(t, "member-99", request.ChannelID)
	require.Equal(t, uint8(1), request.ChannelType)
	require.Equal(t, uint64(10), request.StartMessageSeq)
	require.Equal(t, uint64(20), request.EndMessageSeq)
	require.Equal(t, 30, request.Limit)
	require.Equal(t, PullModeUp, request.PullMode)
}
