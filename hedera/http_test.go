package hedera_helper

import (
	"reflect"
	"testing"

	"github.com/hashgraph/hedera-sdk-go/v2"
)

func TestGetAllSelllerDevicesFromExplorer(t *testing.T) {
	tests := []struct {
		name    string
		want    map[string]string
		wantErr bool
	}{
		{
			name:    "GetAllSelllerDevicesFromExplorer",
			want:    map[string]string{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetAllDevicesFromExplorer()
			if (err != nil) != tt.wantErr {
				t.Errorf("GetAllSelllerDevicesFromExplorer() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetAllSelllerDevicesFromExplorer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetLastMessageFromTopic(t *testing.T) {
	type args struct {
		topicID hedera.TopicID
	}
	topicID, _ := hedera.TopicIDFromString("0.0.3696679")
	tests := []struct {
		name    string
		args    args
		want    HCSMessage
		wantErr bool
	}{
		{
			name: "GetLastMessageFromTopic",
			args: args{
				topicID: topicID,
			},
			want:    HCSMessage{},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetLastMessageFromTopic(tt.args.topicID)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetLastMessageFromTopic() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetLastMessageFromTopic() = %v, want %v", got, tt.want)
			}
		})
	}
}
