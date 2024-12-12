// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package hederacontract

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// HederacontractMetaData contains all meta data concerning the Hederacontract contract.
var HederacontractMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"uint64\",\"name\":\"stdOutTopic\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"stdInTopic\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"stdErrTopic\",\"type\":\"uint64\"},{\"internalType\":\"string\",\"name\":\"peerID\",\"type\":\"string\"},{\"internalType\":\"uint8[]\",\"name\":\"serviceIDs\",\"type\":\"uint8[]\"},{\"internalType\":\"uint8[]\",\"name\":\"prices\",\"type\":\"uint8[]\"}],\"name\":\"putPeerAvailableSelf\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"allowedServices\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getPeerArraySize\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"hederaAddressToPeer\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"available\",\"type\":\"bool\"},{\"internalType\":\"string\",\"name\":\"peerID\",\"type\":\"string\"},{\"internalType\":\"uint64\",\"name\":\"stdOutTopic\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"stdInTopic\",\"type\":\"uint64\"},{\"internalType\":\"uint64\",\"name\":\"stdErrTopic\",\"type\":\"uint64\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"peerList\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
}

// HederacontractABI is the input ABI used to generate the binding from.
// Deprecated: Use HederacontractMetaData.ABI instead.
var HederacontractABI = HederacontractMetaData.ABI

// Hederacontract is an auto generated Go binding around an Ethereum contract.
type Hederacontract struct {
	HederacontractCaller     // Read-only binding to the contract
	HederacontractTransactor // Write-only binding to the contract
	HederacontractFilterer   // Log filterer for contract events
}

// HederacontractCaller is an auto generated read-only Go binding around an Ethereum contract.
type HederacontractCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// HederacontractTransactor is an auto generated write-only Go binding around an Ethereum contract.
type HederacontractTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// HederacontractFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type HederacontractFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// HederacontractSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type HederacontractSession struct {
	Contract     *Hederacontract   // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// HederacontractCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type HederacontractCallerSession struct {
	Contract *HederacontractCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts         // Call options to use throughout this session
}

// HederacontractTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type HederacontractTransactorSession struct {
	Contract     *HederacontractTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts         // Transaction auth options to use throughout this session
}

// HederacontractRaw is an auto generated low-level Go binding around an Ethereum contract.
type HederacontractRaw struct {
	Contract *Hederacontract // Generic contract binding to access the raw methods on
}

// HederacontractCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type HederacontractCallerRaw struct {
	Contract *HederacontractCaller // Generic read-only contract binding to access the raw methods on
}

// HederacontractTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type HederacontractTransactorRaw struct {
	Contract *HederacontractTransactor // Generic write-only contract binding to access the raw methods on
}

// NewHederacontract creates a new instance of Hederacontract, bound to a specific deployed contract.
func NewHederacontract(address common.Address, backend bind.ContractBackend) (*Hederacontract, error) {
	contract, err := bindHederacontract(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Hederacontract{HederacontractCaller: HederacontractCaller{contract: contract}, HederacontractTransactor: HederacontractTransactor{contract: contract}, HederacontractFilterer: HederacontractFilterer{contract: contract}}, nil
}

// NewHederacontractCaller creates a new read-only instance of Hederacontract, bound to a specific deployed contract.
func NewHederacontractCaller(address common.Address, caller bind.ContractCaller) (*HederacontractCaller, error) {
	contract, err := bindHederacontract(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &HederacontractCaller{contract: contract}, nil
}

// NewHederacontractTransactor creates a new write-only instance of Hederacontract, bound to a specific deployed contract.
func NewHederacontractTransactor(address common.Address, transactor bind.ContractTransactor) (*HederacontractTransactor, error) {
	contract, err := bindHederacontract(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &HederacontractTransactor{contract: contract}, nil
}

// NewHederacontractFilterer creates a new log filterer instance of Hederacontract, bound to a specific deployed contract.
func NewHederacontractFilterer(address common.Address, filterer bind.ContractFilterer) (*HederacontractFilterer, error) {
	contract, err := bindHederacontract(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &HederacontractFilterer{contract: contract}, nil
}

// bindHederacontract binds a generic wrapper to an already deployed contract.
func bindHederacontract(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := HederacontractMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Hederacontract *HederacontractRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Hederacontract.Contract.HederacontractCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Hederacontract *HederacontractRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Hederacontract.Contract.HederacontractTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Hederacontract *HederacontractRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Hederacontract.Contract.HederacontractTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Hederacontract *HederacontractCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Hederacontract.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Hederacontract *HederacontractTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Hederacontract.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Hederacontract *HederacontractTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Hederacontract.Contract.contract.Transact(opts, method, params...)
}

// AllowedServices is a free data retrieval call binding the contract method 0x0636b438.
//
// Solidity: function allowedServices(uint256 ) view returns(string)
func (_Hederacontract *HederacontractCaller) AllowedServices(opts *bind.CallOpts, arg0 *big.Int) (string, error) {
	var out []interface{}
	err := _Hederacontract.contract.Call(opts, &out, "allowedServices", arg0)

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// AllowedServices is a free data retrieval call binding the contract method 0x0636b438.
//
// Solidity: function allowedServices(uint256 ) view returns(string)
func (_Hederacontract *HederacontractSession) AllowedServices(arg0 *big.Int) (string, error) {
	return _Hederacontract.Contract.AllowedServices(&_Hederacontract.CallOpts, arg0)
}

// AllowedServices is a free data retrieval call binding the contract method 0x0636b438.
//
// Solidity: function allowedServices(uint256 ) view returns(string)
func (_Hederacontract *HederacontractCallerSession) AllowedServices(arg0 *big.Int) (string, error) {
	return _Hederacontract.Contract.AllowedServices(&_Hederacontract.CallOpts, arg0)
}

// GetPeerArraySize is a free data retrieval call binding the contract method 0xe9b6fb4b.
//
// Solidity: function getPeerArraySize() view returns(uint256)
func (_Hederacontract *HederacontractCaller) GetPeerArraySize(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Hederacontract.contract.Call(opts, &out, "getPeerArraySize")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetPeerArraySize is a free data retrieval call binding the contract method 0xe9b6fb4b.
//
// Solidity: function getPeerArraySize() view returns(uint256)
func (_Hederacontract *HederacontractSession) GetPeerArraySize() (*big.Int, error) {
	return _Hederacontract.Contract.GetPeerArraySize(&_Hederacontract.CallOpts)
}

// GetPeerArraySize is a free data retrieval call binding the contract method 0xe9b6fb4b.
//
// Solidity: function getPeerArraySize() view returns(uint256)
func (_Hederacontract *HederacontractCallerSession) GetPeerArraySize() (*big.Int, error) {
	return _Hederacontract.Contract.GetPeerArraySize(&_Hederacontract.CallOpts)
}

// HederaAddressToPeer is a free data retrieval call binding the contract method 0xc8d3d80b.
//
// Solidity: function hederaAddressToPeer(address ) view returns(bool available, string peerID, uint64 stdOutTopic, uint64 stdInTopic, uint64 stdErrTopic)
func (_Hederacontract *HederacontractCaller) HederaAddressToPeer(opts *bind.CallOpts, arg0 common.Address) (struct {
	Available   bool
	PeerID      string
	StdOutTopic uint64
	StdInTopic  uint64
	StdErrTopic uint64
}, error) {
	var out []interface{}
	err := _Hederacontract.contract.Call(opts, &out, "hederaAddressToPeer", arg0)

	outstruct := new(struct {
		Available   bool
		PeerID      string
		StdOutTopic uint64
		StdInTopic  uint64
		StdErrTopic uint64
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Available = *abi.ConvertType(out[0], new(bool)).(*bool)
	outstruct.PeerID = *abi.ConvertType(out[1], new(string)).(*string)
	outstruct.StdOutTopic = *abi.ConvertType(out[2], new(uint64)).(*uint64)
	outstruct.StdInTopic = *abi.ConvertType(out[3], new(uint64)).(*uint64)
	outstruct.StdErrTopic = *abi.ConvertType(out[4], new(uint64)).(*uint64)

	return *outstruct, err

}

// HederaAddressToPeer is a free data retrieval call binding the contract method 0xc8d3d80b.
//
// Solidity: function hederaAddressToPeer(address ) view returns(bool available, string peerID, uint64 stdOutTopic, uint64 stdInTopic, uint64 stdErrTopic)
func (_Hederacontract *HederacontractSession) HederaAddressToPeer(arg0 common.Address) (struct {
	Available   bool
	PeerID      string
	StdOutTopic uint64
	StdInTopic  uint64
	StdErrTopic uint64
}, error) {
	return _Hederacontract.Contract.HederaAddressToPeer(&_Hederacontract.CallOpts, arg0)
}

// HederaAddressToPeer is a free data retrieval call binding the contract method 0xc8d3d80b.
//
// Solidity: function hederaAddressToPeer(address ) view returns(bool available, string peerID, uint64 stdOutTopic, uint64 stdInTopic, uint64 stdErrTopic)
func (_Hederacontract *HederacontractCallerSession) HederaAddressToPeer(arg0 common.Address) (struct {
	Available   bool
	PeerID      string
	StdOutTopic uint64
	StdInTopic  uint64
	StdErrTopic uint64
}, error) {
	return _Hederacontract.Contract.HederaAddressToPeer(&_Hederacontract.CallOpts, arg0)
}

// PeerList is a free data retrieval call binding the contract method 0xb307b356.
//
// Solidity: function peerList(uint256 ) view returns(address)
func (_Hederacontract *HederacontractCaller) PeerList(opts *bind.CallOpts, arg0 *big.Int) (common.Address, error) {
	var out []interface{}
	err := _Hederacontract.contract.Call(opts, &out, "peerList", arg0)

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// PeerList is a free data retrieval call binding the contract method 0xb307b356.
//
// Solidity: function peerList(uint256 ) view returns(address)
func (_Hederacontract *HederacontractSession) PeerList(arg0 *big.Int) (common.Address, error) {
	return _Hederacontract.Contract.PeerList(&_Hederacontract.CallOpts, arg0)
}

// PeerList is a free data retrieval call binding the contract method 0xb307b356.
//
// Solidity: function peerList(uint256 ) view returns(address)
func (_Hederacontract *HederacontractCallerSession) PeerList(arg0 *big.Int) (common.Address, error) {
	return _Hederacontract.Contract.PeerList(&_Hederacontract.CallOpts, arg0)
}

// PutPeerAvailableSelf is a paid mutator transaction binding the contract method 0x1cf38b6a.
//
// Solidity: function putPeerAvailableSelf(uint64 stdOutTopic, uint64 stdInTopic, uint64 stdErrTopic, string peerID, uint8[] serviceIDs, uint8[] prices) returns()
func (_Hederacontract *HederacontractTransactor) PutPeerAvailableSelf(opts *bind.TransactOpts, stdOutTopic uint64, stdInTopic uint64, stdErrTopic uint64, peerID string, serviceIDs []uint8, prices []uint8) (*types.Transaction, error) {
	return _Hederacontract.contract.Transact(opts, "putPeerAvailableSelf", stdOutTopic, stdInTopic, stdErrTopic, peerID, serviceIDs, prices)
}

// PutPeerAvailableSelf is a paid mutator transaction binding the contract method 0x1cf38b6a.
//
// Solidity: function putPeerAvailableSelf(uint64 stdOutTopic, uint64 stdInTopic, uint64 stdErrTopic, string peerID, uint8[] serviceIDs, uint8[] prices) returns()
func (_Hederacontract *HederacontractSession) PutPeerAvailableSelf(stdOutTopic uint64, stdInTopic uint64, stdErrTopic uint64, peerID string, serviceIDs []uint8, prices []uint8) (*types.Transaction, error) {
	return _Hederacontract.Contract.PutPeerAvailableSelf(&_Hederacontract.TransactOpts, stdOutTopic, stdInTopic, stdErrTopic, peerID, serviceIDs, prices)
}

// PutPeerAvailableSelf is a paid mutator transaction binding the contract method 0x1cf38b6a.
//
// Solidity: function putPeerAvailableSelf(uint64 stdOutTopic, uint64 stdInTopic, uint64 stdErrTopic, string peerID, uint8[] serviceIDs, uint8[] prices) returns()
func (_Hederacontract *HederacontractTransactorSession) PutPeerAvailableSelf(stdOutTopic uint64, stdInTopic uint64, stdErrTopic uint64, peerID string, serviceIDs []uint8, prices []uint8) (*types.Transaction, error) {
	return _Hederacontract.Contract.PutPeerAvailableSelf(&_Hederacontract.TransactOpts, stdOutTopic, stdInTopic, stdErrTopic, peerID, serviceIDs, prices)
}
