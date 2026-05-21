// Copyright (c) 2021-2026 Rustam Gilyazov and Contributors.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package stream

import (
	"context"
	"errors"
	"testing"

	"github.com/rusq/slack"
	"go.uber.org/mock/gomock"

	"github.com/rusq/slackdump/v4/internal/client/mock_client"
)

func TestStream_canvasFile(t *testing.T) {
	tests := []struct {
		name     string
		fileId   string
		expectFn func(ms *mock_client.MockSlack)
		want     *slack.File
		wantErr  bool
	}{
		{
			name:   "file ID is empty",
			fileId: "",
		},
		{
			name:   "getfileinfocontext returns an error",
			fileId: "F123456",
			expectFn: func(ms *mock_client.MockSlack) {
				ms.EXPECT().GetFileInfoContext(gomock.Any(), "F123456", 0, 1).Return(nil, nil, nil, errors.New("getfileinfocontext error"))
			},
			wantErr: true,
		},
		{
			name:   "file not found",
			fileId: "F123456",
			expectFn: func(ms *mock_client.MockSlack) {
				ms.EXPECT().GetFileInfoContext(gomock.Any(), "F123456", 0, 1).Return(nil, nil, nil, nil)
			},
			wantErr: true,
		},
		{
			name:   "success",
			fileId: "F123456",
			expectFn: func(ms *mock_client.MockSlack) {
				ms.EXPECT().
					GetFileInfoContext(gomock.Any(), "F123456", 0, 1).
					Return(&slack.File{ID: "F123456"}, nil, nil, nil)
			},
			want: &slack.File{ID: "F123456"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			ms := mock_client.NewMockSlack(ctrl)
			if tt.expectFn != nil {
				tt.expectFn(ms)
			}
			cs := &Stream{client: ms}
			got, err := cs.canvasFile(context.Background(), tt.fileId)
			if (err != nil) != tt.wantErr {
				t.Errorf("Stream.canvasFile() error = %v, wantErr %v", err, tt.wantErr)
			}
			if (got == nil) != (tt.want == nil) {
				t.Errorf("Stream.canvasFile() got = %v, want %v", got, tt.want)
			}
			if got != nil && tt.want != nil && got.ID != tt.want.ID {
				t.Errorf("Stream.canvasFile() got.ID = %v, want %v", got.ID, tt.want.ID)
			}
		})
	}
}
