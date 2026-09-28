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
	peerID, err := PeerIDFromHederaPublicKey(hederaPublicKey)
	if err != nil {
		log.Fatal(err)
	}
	return peerID
}

// PeerIDFromHederaPublicKey is the non-fatal form of
// ConvertHederaPublicKeyToPeerID, for keys that come from outside our own
// config (the smart contract, the mirror node, topic messages, the seller
// list). It returns an error instead of exiting when the key cannot be parsed
// or is not a secp256k1 key, so one bad registration can't take the node down.
func PeerIDFromHederaPublicKey(hederaPublicKey string) (string, error) {
	hpub, err := hedera.PublicKeyFromString(hederaPublicKey)
	if err != nil {
		return "", err
	}

	libp2pPubKey, err := libp2pcrypto.UnmarshalSecp256k1PublicKey(hpub.BytesRaw())
	if err != nil {
		return "", err
	}

	peerID, err := peer.IDFromPublicKey(libp2pPubKey)
	if err != nil {
		return "", err
	}

	return peerID.String(), nil
}

func ConverHederaPublicKeyToEthereunAddress(hederaPublicKey string) string {
	ethaddress, err := EthereumAddressFromPublicKeyHex(hederaPublicKey)
	if err != nil {
		log.Fatal(err)
	}
	return ethaddress
}

// EthereumAddressFromPublicKeyHex is the non-fatal form of
// ConverHederaPublicKeyToEthereunAddress, for keys that come from the network
// (e.g. a connected peer's libp2p key) rather than from our own config. It returns
// the lowercase address without 0x, or an error if the key is not a compressed
// secp256k1 public key.
func EthereumAddressFromPublicKeyHex(hederaPublicKey string) (string, error) {
	publicKeyBytes, err := hex.DecodeString(hederaPublicKey)
	if err != nil {
		return "", err
	}
	decompressedEdcaPubkey, err := ethcrypto.DecompressPubkey(publicKeyBytes)
	if err != nil {
		return "", err
	}

	ethaddress := ethcrypto.PubkeyToAddress(*decompressedEdcaPubkey)
	// return string with 0x prefix removed
	return strings.ToLower(ethaddress.String()[2:]), nil
}
