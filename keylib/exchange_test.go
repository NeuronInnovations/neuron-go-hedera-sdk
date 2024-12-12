package keylib

import (
	"crypto/rand"
	"fmt"
	"testing"
)

func Test_general(t *testing.T) {

	buyerPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	buyerPub := buyerPriv.PublicKey()

	sellerPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	sellerPub := sellerPriv.PublicKey()

	buyerPubBytes := buyerPub.Bytes()
	sellerPubBytes := sellerPub.Bytes()

	sellerPubParsed, err := curve.NewPublicKey(sellerPubBytes)
	if err != nil {
		panic(err)
	}

	buyerSecret, err := buyerPriv.ECDH(sellerPubParsed)
	if err != nil {
		panic(err)
	}

	buyerPubParsed, err := curve.NewPublicKey(buyerPubBytes)
	if err != nil {
		panic(err)
	}

	sellerSecret, err := sellerPriv.ECDH(buyerPubParsed)
	if err != nil {
		panic(err)
	}

	fmt.Println("Buyer secret:", buyerSecret)
	fmt.Println("Seller secret:", sellerSecret)

	buyerEncrypted, err := encrypt([]byte("hello world"), buyerSecret, runningHash)
	if err != nil {
		panic(err)
	}

	fmt.Println("Buyer encrypted message:", buyerEncrypted)

	sellerDecrypted, err := decrypt(buyerEncrypted, sellerSecret, runningHash)
	if err != nil {
		panic(err)
	}

	fmt.Println("Seller decrypted message:", string(sellerDecrypted))

	encryptedMessage, err := EncryptForOtherside([]byte("hello world"), "....", "02759b048e7ccf6ba68f9658105a4a139b5f9f5dfd451857c600cc28f33a1a99ae")
	if err != nil {
		panic(err)
	}
	fmt.Println("Encrypted message:", encryptedMessage)

	decryptedMessage, err := DecryptFromOtherside(encryptedMessage, "...", "03109b214ce8113ef079ae34977b1f7ae217cdadbb2ddb93442a084212149ceb5d")

	if err != nil {
		panic(err)
	}
	fmt.Println("OMG Decrypted message:", string(decryptedMessage))
}
