package keylib

import (
	"testing"

	"github.com/hashgraph/hedera-sdk-go/v2"
)

func TestConvertHederaPublicKeyToPeerID(t *testing.T) {
	type args struct {
		hederaPublicKey string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "ConvertEthPublicKeyToPeerID",
			args: args{hederaPublicKey: "02759b048e7ccf6ba68f9658105a4a139b5f9f5dfd451857c600cc28f33a1a99ae"},
			want: "16Uiu2HAm3Lkn9NRieuh3UUTWMNthSDumQL9ctTBKxQqdCC79WUSq",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConvertHederaPublicKeyToPeerID(tt.args.hederaPublicKey); got != tt.want {
				t.Errorf("ConvertEthPublicKeyToPeerID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConverHederaPublicKeyToEthereunAddress(t *testing.T) {
	type args struct {
		hederaPublicKey string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "ConvertEthPublicKeyToEthereunAddress",
			args: args{hederaPublicKey: "02759b048e7ccf6ba68f9658105a4a139b5f9f5dfd451857c600cc28f33a1a99ae"},
			want: "e364f2f1e5F4F03d1df682322500b9c68C997ec3",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConverHederaPublicKeyToEthereunAddress(tt.args.hederaPublicKey); got != tt.want {
				t.Errorf("ConverHederaPublicKeyToEthereunAddress() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPeerIDFromHederaPublicKey(t *testing.T) {
	got, err := PeerIDFromHederaPublicKey("02759b048e7ccf6ba68f9658105a4a139b5f9f5dfd451857c600cc28f33a1a99ae")
	if err != nil || got != "16Uiu2HAm3Lkn9NRieuh3UUTWMNthSDumQL9ctTBKxQqdCC79WUSq" {
		t.Fatalf("secp256k1 key: got %q, %v", got, err)
	}

	ed, err := hedera.PrivateKeyGenerateEd25519()
	if err != nil {
		t.Fatal(err)
	}
	for name, key := range map[string]string{
		"ed25519": ed.PublicKey().StringRaw(),
		"garbage": "not-a-key",
		"empty":   "",
	} {
		if pid, err := PeerIDFromHederaPublicKey(key); err == nil {
			t.Errorf("%s: expected an error, got peer ID %q", name, pid)
		}
	}
}
