package keylib

import (
	"encoding/hex"
	"log"
	"strings"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/hashgraph/hedera-sdk-go/v2"
	"github.com/libp2p/go-libp2p/core/peer"
)

func ConvertHederaPublicKeyToPeerID(hederaPublicKey string) string {
	hpub, err := hedera.PublicKeyFromString(hederaPublicKey)

	if err != nil {
		log.Fatal(err)
	}

	libp2pPubKey, err := libp2pcrypto.UnmarshalSecp256k1PublicKey(hpub.BytesRaw())
	if err != nil {
		panic(err)
	}

	peerID, err := peer.IDFromPublicKey(libp2pPubKey)
	if err != nil {
		panic(err)
	}

	return peerID.String()

}

func ConverHederaPublicKeyToEthereunAddress(hederaPublicKey string) string {

	publicKeyBytes, err := hex.DecodeString(hederaPublicKey)
	if err != nil {
		log.Fatal(err)
	}
	decompressedEdcaPubkey, err := ethcrypto.DecompressPubkey(publicKeyBytes)
	if err != nil {
		log.Fatal(err)
	}

	ethaddress := ethcrypto.PubkeyToAddress(*decompressedEdcaPubkey)
	// return string with 0x prefix removed
	return strings.ToLower(ethaddress.String()[2:])
}
