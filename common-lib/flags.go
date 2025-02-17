package commonlib

import flag "github.com/spf13/pflag"

// PeerOrRelayFlag specifies whether the node should operate in "peer" mode (for regular P2P communication)
// or "relay" mode (to relay traffic for other peers). Default is "peer".
var PeerOrRelayFlag *string

// PortFlag specifies the port the node should bind its listener to. Default is "0", which means a random port.
// TODO: 0 is not fully iplemented yet
var PortFlag *string

// EnableUpPNPFlag enables Universal Plug and Play (UPnP) to automatically forward ports on supported routers,
// making the node more accessible. Default is false (disabled).
var EnableUpPNPFlag *bool

// ForceProtocolFlag forces the application to use a specific network protocol, either "udp" or "tcp". Default is "udp".
// TODO: only udp and QUIC supported at the moment
var ForceProtocolFlag *string

// ForceLocationFlag allows overriding the node's location using a JSON struct in the format:
// {"lat": <latitude>, "lon": <longitude>, "alt": <altitude>}. Default is empty. This overrides
// whatever is in the environment file
var ForceLocationFlag *string

// BuyerOrSellerFlag determines whether the node is operating as a "buyer" (data requester)
// or "seller" (data provider). Default is empty, requiring explicit configuration.
var BuyerOrSellerFlag *string

// MyPublicIpFlag allows the user to manually set the public IP address of the node.
// This is useful if automatic detection fails or a specific IP is required. Default is empty.
var MyPublicIpFlag *string

// MyPublicPortFlag allows the user to manually set the public port of the node.
// Useful for specifying externally mapped ports in NAT or firewall scenarios. Default is empty.
var MyPublicPortFlag *string

// ListOfSellersSourceFlag specifies the source for obtaining the list of sellers.
// Options are "explorer" (fetch from an external service) or "env" (fetch from environment variables). Default is "explorer".
var ListOfSellersSourceFlag *string

// RadiusFlag specifies the radius (in kilometers) to restrict seller nodes based on their latitude and longitude
// when the node is in buyer mode and when the ListOfSellersSourceFlag is set to "explorer".
var RadiusFlag *int

// ClearCacheFlag specifies whether the cache should be cleared before starting the node. Default is false.
var ClearCacheFlag *bool

func InitFlags() {
	PeerOrRelayFlag = flag.String("mode", "peer", "Select peer, or relay")
	ForceProtocolFlag = flag.String("force-protocol", "udp", "Force protocol: udp or tcp")
	ForceLocationFlag = flag.String("force-location", "", `force location using the json struct  {"lat": 0, "lon": 0, "alt": 0}`)
	PortFlag = flag.String("port", "0", "Port listener to bind to")
	EnableUpPNPFlag = flag.Bool("enable-upnp", false, "enable upnp")
	BuyerOrSellerFlag = flag.String("buyer-or-seller", "", "Direction of data flow is from sell to buy")
	MyPublicIpFlag = flag.String("my-public-ip", "", "set my public ip")
	MyPublicPortFlag = flag.String("my-public-port", "", "set my public port")
	ListOfSellersSourceFlag = flag.String("list-of-sellers-source", "explorer", "set where the list of sellers is to be obtained from:'explorer' or 'env'")
	RadiusFlag = flag.Int("radius", 1, "set radius in Km to restrict seller lat lon when in buyer mode")
	ClearCacheFlag = flag.Bool("clear-cache", false, "set clear cache flag to delete cache be")
	flag.CommandLine.ParseErrorsWhitelist.UnknownFlags = true
	flag.Parse()
}
