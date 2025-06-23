package keylib

import (
	"testing"
)

func TestConvertHederaPublicKeyToPeerID(t *testing.T) {
	type args struct {
		hederaPublicKey string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "ConvertEthPublicKeyToPeerID",
			args: args{hederaPublicKey: "02759b048e7ccf6ba68f9658105a4a139b5f9f5dfd451857c600cc28f33a1a99ae"},
			want: "16Uiu2HAm3Lkn9NRieuh3UUTWMNthSDumQL9ctTBKxQqdCC79WUSq",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertHederaPublicKeyToPeerID(tt.args.hederaPublicKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("ConvertHederaPublicKeyToPeerID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ConvertHederaPublicKeyToPeerID() = %v, want %v", got, tt.want)
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
