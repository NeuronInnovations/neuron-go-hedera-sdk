package hedera_helper

import (
	"math/big"
	"os"
	"reflect"
	"testing"

	"github.com/joho/godotenv"
)

func init() {

	// load the environment variables
	err := godotenv.Load("../.env")
	if err != nil {
		panic(err)
	}
}

func TestGetPeerArraySize(t *testing.T) {
	tests := []struct {
		name    string
		want    *big.Int
		wantErr bool
	}{
		{
			name:    "GetPeerArraySize",
			want:    big.NewInt(20),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetPeerArraySize()
			if (err != nil) != tt.wantErr {
				t.Errorf("GetPeerArraySize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetPeerArraySize() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getPeerhederaAccEvmAddress(t *testing.T) {
	type args struct {
		hederaAccEvmAddress string
	}
	tests := []struct {
		name string
		args args
		want struct {
			PeerID      string
			StdOutTopic uint64
			StdInTopic  uint64
		}
		wantErr bool
	}{
		{
			name: "getPeerhederaAccEvmAddress",
			args: args{
				hederaAccEvmAddress: os.Getenv("hedera_evm_id"),
			},
			want: struct {
				PeerID      string
				StdOutTopic uint64
				StdInTopic  uint64
			}{
				PeerID:      "16Uiu2HAmDmp57BopASBwMfQ2cL9hc7V5K5S5Rdd9bHdP8geeCTZE",
				StdOutTopic: 2669666,
				StdInTopic:  2669667,
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetPeerInfo(tt.args.hederaAccEvmAddress)
			if (err != nil) != tt.wantErr {
				t.Errorf("getPeerhederaAccEvmAddress() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getPeerhederaAccEvmAddress() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetAllPeers(t *testing.T) {
	tests := []struct {
		name    string
		want    bool
		wantErr bool
	}{
		{
			name:    "GetAllPeers",
			want:    true,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peerList, err := GetAllPeers()
			if err != nil {
				t.Errorf("GetAllPeers() error = %v", err)
				return
			}
			got := len(peerList) > 0
			if (err != nil) != tt.wantErr {
				t.Errorf("GetAllPeers() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetAllPeers() = %v, want %v", got, tt.want)
			}
		})
	}
}
