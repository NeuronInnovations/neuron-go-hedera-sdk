package keylib

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"encoding/hex"

	"github.com/ethereum/go-ethereum/crypto"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

var runningHashLong = []byte("yakfOMkPmf13a75EhWE795l9+be6/xcB+Duba5kvRfBHHqtCnUFYvKZlxLWFtVJQ")

var runningHash = runningHashLong[len(runningHashLong)-16:]

var curve = ecdh.P256()

func EncryptForOtherside(message []byte, myPrivateKeyETH string, otherPubliKeyHedera string) ([]byte, error) {

	otherPubliKeyHex, _ := hex.DecodeString(otherPubliKeyHedera)
	publicKeyECDSA, err := ethcrypto.DecompressPubkey(otherPubliKeyHex)
	if err != nil {
		panic(err)
	}

	privateKeyECDCA, err := ethcrypto.HexToECDSA(myPrivateKeyETH)
	if err != nil {
		panic(err)
	}

	sharedSecret, _ := crypto.S256().ScalarMult(publicKeyECDSA.X, publicKeyECDSA.Y, privateKeyECDCA.D.Bytes())

	return encrypt(message, sharedSecret.Bytes(), runningHash)
}

func DecryptFromOtherside(message []byte, myPrivateKeyEth string, otherPubliKeyHedera string) ([]byte, error) {
	otherPubliKeyHex, _ := hex.DecodeString(otherPubliKeyHedera)

	publicKeyECDSA, err := ethcrypto.DecompressPubkey(otherPubliKeyHex)
	if err != nil {
		panic(err)
	}

	privateKeyECDCA, err := ethcrypto.HexToECDSA(myPrivateKeyEth)
	if err != nil {
		panic(err)
	}

	sharedSecret, _ := crypto.S256().ScalarMult(publicKeyECDSA.X, publicKeyECDSA.Y, privateKeyECDCA.D.Bytes())

	return decrypt(message, sharedSecret.Bytes(), runningHash)
}

func encrypt(message []byte, secret []byte, hashOfSeqNo []byte) ([]byte, error) {
	block, err := aes.NewCipher(secret)
	if err != nil {
		return nil, err
	}
	cfb := cipher.NewCFBEncrypter(block, hashOfSeqNo)
	cipherText := make([]byte, len(message))
	cfb.XORKeyStream(cipherText, message)
	return cipherText, nil
}

func decrypt(message, secret []byte, hashOfSeqNo []byte) ([]byte, error) {
	block, err := aes.NewCipher(secret)
	if err != nil {
		return nil, err
	}
	cfb := cipher.NewCFBDecrypter(block, hashOfSeqNo)
	decrypted := make([]byte, len(message))
	cfb.XORKeyStream(decrypted, message)
	return decrypted, nil
}
