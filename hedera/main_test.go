package hedera_helper

import (
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/joho/godotenv"
	"github.com/multiformats/go-multiaddr"
)

func init() {

	err := godotenv.Load("../.env")
	if err != nil {
		panic(err)
	}
}

func TestGetHRpcClient(t *testing.T) {
	tests := []struct {
		name string
		want *ethclient.Client
	}{
		// TODO: Add test cases.
		{
			name: "GetHRpcClient",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetHRpcClient(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetHRpcClient() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSendScheduledServiceRequestPayment(t *testing.T) {
	type args struct {
		fromHPublicAddress []multiaddr.Multiaddr
		fromEthAddress     string
		toEthAddress       string
		arbiterEthAddress  string
		amount             int64
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "SendScheduledServiceRequestPayment",
			args: args{
				fromHPublicAddress: []multiaddr.Multiaddr{multiaddr.StringCast("/dnsaddr/bootstrap.libp2p.io/p2p/QmbLHAnMoJPWSCR5Zhtx6BHJX9KiKNN6tpvbUcqanj75Nb")},
				fromEthAddress:     "e2436b1e019e993215e832762f9242020d199940",
				toEthAddress:       "e364f2f1e5f4f03d1df682322500b9c68c997ec3",
				arbiterEthAddress:  "27a99572b517fc0646ccb802afa37afc140271ba",
				amount:             2,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := BuyerPrepareServiceRequest(tt.args.fromHPublicAddress, tt.args.fromEthAddress, tt.args.toEthAddress, tt.args.arbiterEthAddress, tt.args.amount); (err != nil) != tt.wantErr {
				t.Errorf("SendScheduledServiceRequestPayment() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCreateAccountFromParent(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "CreateAccountFromParent",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			CreateAccountFromParent()
		})
	}
}
func TestGetDeviceParent(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "GetDeviceParent",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			GetDeviceParent("0x8afdbb083836f23a9733be332007ad6d9ace50c7")
		})
	}
}
