// Copyright 2025 Cockroach Labs Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package scanner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseAuthLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    *authFields
		wantErr string
	}{
		{
			"skip",
			`I250407 13:53:16.585256 4019 util/log/file_sync_buffer.go:237 ⋮ [config]   log format (utf8=✓): crdb-v2`,
			nil,
			"",
		},
		{
			"client_authentication_ok",
			`I250407 20:22:58.997928 731936 4@util/log/event_log.go:39 ⋮ [T1,Vsystem,n1,client=127.0.0.1:53480,hostssl,user=‹roachprod›] 15 ={"Timestamp":1744057378997924809,"EventType":"client_authentication_ok","InstanceID":1,"Network":"tcp","RemoteAddress":"‹127.0.0.1:53480›","SessionID":"183422f21ecccb950000000000000001","Transport":"hostssl","User":"‹roachprod›","SystemIdentity":"‹roachprod›","Method":"cert-password"}`,
			&authFields{
				EventType:      "client_authentication_ok",
				InstanceID:     1,
				Method:         "cert-password",
				SystemIdentity: "roachprod",
				Timestamp:      1744057378997924809,
				Transport:      "hostssl",
				User:           "roachprod",
			},
			"",
		},
		{
			"client_authentication_ok_notransport",
			`I250407 20:22:58.997928 731936 4@util/log/event_log.go:39 ⋮ [T1,Vsystem,n1,client=127.0.0.1:53480,hostssl,user=‹roachprod›] 15 ={"Timestamp":1744057378997924809,"EventType":"client_authentication_ok","InstanceID":1,"Network":"tcp","RemoteAddress":"‹127.0.0.1:53480›","SessionID":"183422f21ecccb950000000000000001","User":"‹roachprod›","SystemIdentity":"‹roachprod›","Method":"cert-password"}`,
			&authFields{
				EventType:      "client_authentication_ok",
				InstanceID:     1,
				Method:         "cert-password",
				SystemIdentity: "roachprod",
				Timestamp:      1744057378997924809,
				Transport:      "",
				User:           "roachprod",
			},
			"",
		},
		{
			"client_session_end",
			`I250407 20:23:07.775793 731934 4@util/log/event_log.go:39 ⋮ [T1,Vsystem,n1,client=127.0.0.1:53480,hostssl,user=‹roachprod›] 16 ={"Timestamp":1744057387775787750,"EventType":"client_session_end","InstanceID":1,"Network":"tcp","RemoteAddress":"‹127.0.0.1:53480›","SessionID":"183422f21ecccb950000000000000001","Duration":8782392793}`,
			&authFields{
				EventType:  "client_session_end",
				InstanceID: 1,
				Timestamp:  1744057387775787750,
			},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := assert.New(t)
			res, err := parseAuthLine([]byte(tt.line))
			if tt.wantErr != "" {
				a.EqualError(err, tt.wantErr)
			}
			a.NoError(err)
			a.Equal(tt.want, res)
		})
	}
}
