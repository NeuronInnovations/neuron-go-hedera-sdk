/*

Code and comments from https://github.com/pion/stun/   and re-released under the same licence.
modified to address our needs.

NAT mapping behaviour (RFC 4787 section 4)
For each request your NAT will create a temporary mapping (hole). These are the different rules for creating and maintaining it.

endpoint independent If you send two UDP packets from the same port to different places on the other side of the NAT, the NAT will use the same port to send those packets. This is the only RTC-friendly mapping behavior, and RFC 4787 requires it as a best practice (REQ-1) on all NATs. If both NATs do this, ICE will be able to connect directly, for all of the filtering behaviors described below. This is the most important setting to get right. This mapping type corresponds to the "cone NATs" in the classic STUN defined in RFC 3489 section 5.

address dependent and address and port dependent If you send two UDP packets from the same port to different places on the other side of the NAT, the NAT will use the same port to send those packets ff the destination address is the same; the destination port does not matter. Sending to two different hosts will use different ports. This will require the use of a TURN server if both sides are using it, unless neither side is doing port-specific filtering, and at least one is doing endpoint independent filtering. If you want webrtc or anything else that uses P2P UDP networking, do not configure your NAT like this. This mapping type (loosely) corresponds to the "symmetric NATs" in the classic STUN defined in RFC 3489 section 5.

NAT filtering behavior (RFC 4787 section 5)
Each hole punch will also have rules around what external traffic is accepted (and routed back to the hole creator).

endpoint independent This is the most permissive of the three. Once you have sent a UDP packet to anywhere on the other side of the NAT, anything else on the other side of the NAT can send UDP packets back to you on that port. This filtering policy gives sysadmins cold sweats, but RFC 4787 recommends its use when real-time-communication (or other things that require "application transparency"; eg gaming) is a priority. Note that this will not do a very good job of compensating if your NAT's mapping behavior is misconfigured. It is more important to get the mapping behavior right.

address dependent This is a middle ground that sysadmins have an easier time justifying, but my impression is that it is harder to configure. Once you have sent a UDP packet to a host on the other side of the NAT, the NAT will allow return UDP traffic from that host, regardless of the port that host sends from, but will not allow inbound UDP traffic from other addresses. If your mapping behavior is configured appropriately, this should function as well as endpoint independent filtering.

address and port dependent This is the strictest of the three. Your NAT will only allow return traffic from exactly where you sent your UDP packet. Using this is not recommended, even if you configure mapping behavior correctly, because it will work poorly when the other NAT is misconfigured (fairly common).

// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

*/

package whoami

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/pion/logging"
	"github.com/pion/stun"
)

type stunServerConn struct {
	conn        net.PacketConn
	LocalAddr   net.Addr
	RemoteAddr  *net.UDPAddr
	OtherAddr   *net.UDPAddr
	messageChan chan *stun.Message
}

func (c *stunServerConn) Close() error {
	return c.conn.Close()
}

const (
	messageHeaderSize = 20
)

var (
	NatDeviceType   string
	NatIPAddress    string
	NatPort         int
	NatReachability bool
)

var (
	addrStrPtr         = "stun.voipgate.com:3478"
	timeoutPtr         = 3
	log                logging.LeveledLogger
	errResponseMessage = errors.New("error reading from response message channel")
	errTimedOut        = errors.New("timed out waiting for response")
	errNoOtherAddress  = errors.New("no OTHER-ADDRESS in message")
)

func GetNatInfoAndUpdateGlobals(portFlag *string) (string, string, int, bool) {
	natype, mapDesc, filterDesc, ip, stunRetrievedPort, err := getNatInfo(*portFlag)
	if err != nil {
		panic(err)
	}

	// Compute natDeviceType and natReachability values
	natDeviceType := natype + ":" + mapDesc + ":" + filterDesc
	natIPAddress := ip
	natPort, _ := strconv.Atoi(*portFlag)
	natReachability := natype == "no-nat" ||
		(mapDesc == "endpoint-independent" && filterDesc == "endpoint-independent") ||
		(mapDesc == "endpoint-independent" && filterDesc == "address-dependent")

	// Optionally, print stunRetrievedPort for logging purposes
	fmt.Println("STUN Retrieved Port:", stunRetrievedPort)
	fmt.Println("Reachability", natIPAddress, natPort, stunRetrievedPort, natDeviceType, "reacheable:", natReachability)

	// Return the computed values

	// Update globals
	NatDeviceType = natDeviceType
	NatIPAddress = natIPAddress
	NatPort = natPort
	NatReachability = natReachability

	return natDeviceType, natIPAddress, natPort, natReachability
}

func getNatInfo(bind string) (string, string, string, string, int, error) {
	log = logging.NewDefaultLeveledLoggerForScope("", logging.LogLevelInfo, os.Stdout)
	mappingResult, mappingdetail, ipAddr, port, err := mappingTests(addrStrPtr, bind)
	filterdatail, _ := filteringTests(addrStrPtr, bind)
	return mappingResult, mappingdetail, filterdatail, ipAddr, port, err
}

// RFC5780: 4.3.  Determining NAT Mapping Behavior
func mappingTests(addrStr string, bind string) (string, string, string, int, error) {
	mapTestConn, err := connect(addrStr, bind)
	if err != nil {
		log.Warnf("Error creating STUN connection: %s", err)
		return "", "", "", -1, err
	}
	defer mapTestConn.Close()

	// Test I: Regular binding request
	log.Info("Mapping Test I: Regular binding request")
	request := stun.MustBuild(stun.TransactionID, stun.BindingRequest)

	resp, err := mapTestConn.roundTrip(request, mapTestConn.RemoteAddr)
	if err != nil {
		return "", "", "", -1, err
	}

	// Parse response message for XOR-MAPPED-ADDRESS and make sure OTHER-ADDRESS valid
	resps1 := parse(resp)
	if resps1.xorAddr == nil || resps1.otherAddr == nil {
		log.Info("Error: NAT discovery feature not supported by this server")
		return "", "", "", -1, errNoOtherAddress
	}
	addr, err := net.ResolveUDPAddr("udp4", resps1.otherAddr.String())
	if err != nil {
		log.Infof("Failed resolving OTHER-ADDRESS: %v", resps1.otherAddr)
		return "", "", "", -1, err
	}

	mapTestConn.OtherAddr = addr
	log.Infof("Received XOR-MAPPED-ADDRESS: %v", resps1.xorAddr)
	log.Infof("Received MAPPED-ADDRESS: %v", resps1.mappedAddr)

	// Assert mapping behavior
	if resps1.xorAddr.String() == mapTestConn.LocalAddr.String() {
		log.Warn("=> NAT mapping behavior: endpoint independent (no NAT)")
		return "no-nat", "endpoint-independent", resps1.xorAddr.IP.String(), resps1.xorAddr.Port, nil
	}

	// Test II: Send binding request to the other address but primary port
	log.Info("Mapping Test II: Send binding request to the other address but primary port")
	oaddr := *mapTestConn.OtherAddr
	oaddr.Port = mapTestConn.RemoteAddr.Port
	resp, err = mapTestConn.roundTrip(request, &oaddr)
	if err != nil {
		return "", "", "", -1, err
	}

	// Assert mapping behavior
	resps2 := parse(resp)
	log.Infof("Received XOR-MAPPED-ADDRESS: %v", resps2.xorAddr)
	if resps2.xorAddr.String() == resps1.xorAddr.String() {
		log.Warn("=> NAT mapping behavior: endpoint independent")
		return "cone", "endpoint-independent", resps2.xorAddr.IP.String(), resps2.xorAddr.Port, nil
	}

	// Test III: Send binding request to the other address and port
	log.Info("Mapping Test III: Send binding request to the other address and port")
	resp, err = mapTestConn.roundTrip(request, mapTestConn.OtherAddr)
	if err != nil {
		return "", "", "", -1, err
	}

	// Assert mapping behavior
	resps3 := parse(resp)
	var res1, res2 string

	log.Infof("Received XOR-MAPPED-ADDRESS: %v", resps3.xorAddr)
	if resps3.xorAddr.String() == resps2.xorAddr.String() {
		res1 = "port-restricted-cone"
		res2 = "address-dependent"
		log.Warn("=> NAT mapping behavior: address dependent")
	} else {
		res1 = "symmetric"
		res2 = "address-dependent and port dependent"
		log.Warn("=> NAT mapping behavior: address and port dependent")
	}

	return res1, res2, resps3.xorAddr.IP.String(), resps3.xorAddr.Port, mapTestConn.Close()
}

// RFC5780: 4.4.  Determining NAT Filtering Behavior
func filteringTests(addrStr string, bind string) (string, error) {
	mapTestConn, err := connect(addrStr, bind)
	if err != nil {
		log.Warnf("Error creating STUN connection: %s", err)
		return "", err
	}

	// Test I: Regular binding request
	log.Info("Filtering Test I: Regular binding request")
	request := stun.MustBuild(stun.TransactionID, stun.BindingRequest)

	resp, err := mapTestConn.roundTrip(request, mapTestConn.RemoteAddr)
	if err != nil || errors.Is(err, errTimedOut) {
		return "", err
	}
	defer mapTestConn.Close()

	resps := parse(resp)
	if resps.xorAddr == nil || resps.otherAddr == nil {
		log.Warn("Error: NAT discovery feature not supported by this server")
		return "", errNoOtherAddress
	}
	addr, err := net.ResolveUDPAddr("udp4", resps.otherAddr.String())
	if err != nil {
		log.Infof("Failed resolving OTHER-ADDRESS: %v", resps.otherAddr)
		return "", err
	}
	mapTestConn.OtherAddr = addr

	// Test II: Request to change both IP and port
	log.Info("Filtering Test II: Request to change both IP and port")
	request = stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	request.Add(stun.AttrChangeRequest, []byte{0x00, 0x00, 0x00, 0x06})

	resp, err = mapTestConn.roundTrip(request, mapTestConn.RemoteAddr)
	if err == nil {
		parse(resp) // just to print out the resp
		log.Warn("=> NAT filtering behavior: endpoint independent")
		return "endpoint-independent", nil
	} else if !errors.Is(err, errTimedOut) {
		return "", err // something else went wrong
	}

	// Test III: Request to change port only
	log.Info("Filtering Test III: Request to change port only")
	request = stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	request.Add(stun.AttrChangeRequest, []byte{0x00, 0x00, 0x00, 0x02})

	resp, err = mapTestConn.roundTrip(request, mapTestConn.RemoteAddr)
	var res string
	if err == nil {
		parse(resp) // just to print out the resp
		log.Warn("=> NAT filtering behavior: address dependent")
		res = "address-dependent"
	} else if errors.Is(err, errTimedOut) {
		log.Warn("=> NAT filtering behavior: address and port dependent")
		res = "address-and-port-dependent"
	}

	return res, mapTestConn.Close()
}

// Parse a STUN message
func parse(msg *stun.Message) (ret struct {
	xorAddr    *stun.XORMappedAddress
	otherAddr  *stun.OtherAddress
	respOrigin *stun.ResponseOrigin
	mappedAddr *stun.MappedAddress
	software   *stun.Software
},
) {
	ret.mappedAddr = &stun.MappedAddress{}
	ret.xorAddr = &stun.XORMappedAddress{}
	ret.respOrigin = &stun.ResponseOrigin{}
	ret.otherAddr = &stun.OtherAddress{}
	ret.software = &stun.Software{}
	if ret.xorAddr.GetFrom(msg) != nil {
		ret.xorAddr = nil
	}
	if ret.otherAddr.GetFrom(msg) != nil {
		ret.otherAddr = nil
	}
	if ret.respOrigin.GetFrom(msg) != nil {
		ret.respOrigin = nil
	}
	if ret.mappedAddr.GetFrom(msg) != nil {
		ret.mappedAddr = nil
	}
	if ret.software.GetFrom(msg) != nil {
		ret.software = nil
	}

	log.Debugf("%v", msg)
	log.Debugf("\tMAPPED-ADDRESS:     %v", ret.mappedAddr)
	log.Debugf("\tXOR-MAPPED-ADDRESS: %v", ret.xorAddr)
	log.Debugf("\tRESPONSE-ORIGIN:    %v", ret.respOrigin)
	log.Debugf("\tOTHER-ADDRESS:      %v", ret.otherAddr)
	log.Debugf("\tSOFTWARE: %v", ret.software)
	for _, attr := range msg.Attributes {
		switch attr.Type {
		case
			stun.AttrXORMappedAddress,
			stun.AttrOtherAddress,
			stun.AttrResponseOrigin,
			stun.AttrMappedAddress,
			stun.AttrSoftware:
			break //nolint:staticcheck
		default:
			log.Debugf("\t%v (l=%v)", attr, attr.Length)
		}
	}
	return ret
}

// Given an address string, returns a StunServerConn
func connect(addrStr string, bind string) (*stunServerConn, error) {
	log.Infof("Connecting to STUN server: %s", addrStr)
	addr, err := net.ResolveUDPAddr("udp4", addrStr)
	if err != nil {
		log.Warnf("Error resolving address: %s", err)
		return nil, err
	}

	bindUdpAddr, err := net.ResolveUDPAddr("udp4", ":"+bind)
	if err != nil {
		return nil, err
	}
	c, err := net.ListenUDP("udp4", bindUdpAddr)
	if err != nil {
		return nil, err
	}
	//defer c.Close()
	log.Infof("Local address: %s", c.LocalAddr())
	log.Infof("Remote address: %s", addr.String())

	mChan := listen(c)

	return &stunServerConn{
		conn:        c,
		LocalAddr:   c.LocalAddr(),
		RemoteAddr:  addr,
		messageChan: mChan,
	}, nil
}

// Send request and wait for response or timeout
func (c *stunServerConn) roundTrip(msg *stun.Message, addr net.Addr) (*stun.Message, error) {
	_ = msg.NewTransactionID()
	log.Infof("Sending to %v: (%v bytes)", addr, msg.Length+messageHeaderSize)
	log.Debugf("%v", msg)
	for _, attr := range msg.Attributes {
		log.Debugf("\t%v (l=%v)", attr, attr.Length)
	}
	_, err := c.conn.WriteTo(msg.Raw, addr)
	if err != nil {
		log.Warnf("Error sending request to %v", addr)
		return nil, err
	}

	// Wait for response or timeout
	select {
	case m, ok := <-c.messageChan:
		if !ok {
			return nil, errResponseMessage
		}
		return m, nil
	case <-time.After(time.Duration(timeoutPtr) * time.Second):
		log.Infof("Timed out waiting for response from server %v", addr)
		return nil, errTimedOut
	}
}

// see: https://github.com/pion/stun/blob/master/cmd/stun-traversal/main.go
func listen(conn *net.UDPConn) (messages chan *stun.Message) {
	messages = make(chan *stun.Message)
	go func() {
		for {
			buf := make([]byte, 1024)

			n, addr, err := conn.ReadFromUDP(buf)
			if err != nil {
				close(messages)
				return
			}
			log.Infof("Response from %v: (%v bytes)", addr, n)
			buf = buf[:n]

			m := new(stun.Message)
			m.Raw = buf
			err = m.Decode()
			if err != nil {
				log.Infof("Error decoding message: %v", err)
				close(messages)
				return
			}

			messages <- m
		}
	}()
	return
}
