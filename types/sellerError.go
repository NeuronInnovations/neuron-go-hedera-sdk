package types

import (
	commonlib "github.com/NeuronInnovations/neuron-go-hedera-sdk/common-lib"
)

type SellerError struct {
	ErrorType     commonlib.ErrorType
	Error         string
	RecoverAction commonlib.RecoverAction
}
