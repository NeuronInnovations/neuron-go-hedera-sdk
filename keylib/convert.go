package keylib

import (
	"encoding/hex"
	"fmt"
	"log"
	"strings"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/hashgraph/hedera-sdk-go/v2"
	"github.com/libp2p/go-libp2p/core/peer"
)

func ConvertHederaPublicKeyToPeerID(hederaPublicKey string) (string, error) {
	hpub, err := hedera.PublicKeyFromString(hederaPublicKey)
	if err != nil {
		return "", fmt.Errorf("error converting hedera public key to peer ID: %v", err)
	}

	libp2pPubKey, err := libp2pcrypto.UnmarshalSecp256k1PublicKey(hpub.BytesRaw())
	if err != nil {
		return "", fmt.Errorf("error unmarshaling public key: %v", err)
	}

	peerID, err := peer.IDFromPublicKey(libp2pPubKey)
	if err != nil {
		return "", fmt.Errorf("error creating peer ID: %v", err)
	}

	return peerID.String(), nil
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

// IsValidEthereumAddress validates if the given string is a valid Ethereum address format
func IsValidEthereumAddress(address string) bool {
	// Check if address starts with "0x" and has correct length (0x + 40 hex characters = 42 total)
	if len(address) != 42 || !strings.HasPrefix(address, "0x") {
		return false
	}

	// Check if the remaining characters are valid hex
	for i := 2; i < len(address); i++ {
		c := address[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}

	return true
}
