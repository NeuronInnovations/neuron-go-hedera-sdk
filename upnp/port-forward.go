package upnp

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

/*
In linux use this to discover UPnP devices.
echo -ne "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 10\r\nST: urn:schemas-upnp-org:service:WANIPConnection:1\r\n\r\n" | nc -u -w 1 239.255.255.250 1900 && nc -u -l -p 1900
*/
const ssdpRequest = "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 10\r\nST: urn:schemas-upnp-org:service:WANIPConnection:1\r\n\r\n"

// SendSSDPRequest sends an SSDP M-SEARCH request and returns the control URL (Location) of the first UPnP device it finds
func SendSSDPRequest() (string, error) {
	// Set the UDP address for the SSDP multicast group
	addr, err := net.ResolveUDPAddr("udp4", "239.255.255.250:1900")
	if err != nil {
		return "", fmt.Errorf("could not resolve UDP address: %v", err)
	}

	// Create a UDP connection
	conn, err := net.ListenPacket("udp4", ":0") // Dynamic port
	if err != nil {
		return "", fmt.Errorf("could not listen on UDP socket: %v", err)
	}
	defer conn.Close()

	// Send SSDP discovery request
	_, err = conn.WriteTo([]byte(ssdpRequest), addr)
	if err != nil {
		return "", fmt.Errorf("could not send SSDP request: %v", err)
	}

	// Set a read deadline
	conn.SetReadDeadline(time.Now().Add(11 * time.Second))

	// Buffer to read the response
	buf := make([]byte, 1024)

	// Read the response
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		return "", fmt.Errorf("error reading from UDP, upnp device may not exist at all: %v", err)
	}

	// Convert the response to a string and process it
	response := string(buf[:n])

	// Check if the response contains the WANIPConnection service type
	if !strings.Contains(response, "urn:schemas-upnp-org:service:WANIPConnection:1") {
		return "", fmt.Errorf("no UPnP port forwarding capable router found in SSDP response")
	}

	// Extract the LOCATION header (control URL)
	lines := strings.Split(response, "\r\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "LOCATION:") {
			// Trim "LOCATION: " and return the URL
			location := strings.TrimSpace(line[len("LOCATION:"):])
			return location, nil
		}
	}

	return "", fmt.Errorf("no LOCATION field found in SSDP response")
}

// AddPortMapping sends a SOAP request to the UPnP control URL to add a port mapping
func AddPortMapping(controlURL string, externalPort, internalPort int, protocol, description string) error {
	// Get the internal IP address of the local machine
	internalIP, err := getLocalIP()
	if err != nil {
		return fmt.Errorf("failed to get local IP: %v", err)
	}

	soapBody := fmt.Sprintf(`<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
  <s:Body>
    <u:AddPortMapping xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">
      <NewRemoteHost></NewRemoteHost>
      <NewExternalPort>%d</NewExternalPort>
      <NewProtocol>%s</NewProtocol>
      <NewInternalPort>%d</NewInternalPort>
      <NewInternalClient>%s</NewInternalClient>
      <NewEnabled>1</NewEnabled>
      <NewPortMappingDescription>%s</NewPortMappingDescription>
      <NewLeaseDuration>0</NewLeaseDuration>
    </u:AddPortMapping>
  </s:Body>
</s:Envelope>`, externalPort, protocol, internalPort, internalIP, description)

	return sendSOAPRequest(controlURL, soapBody, "AddPortMapping")
}

// getLocalIP finds the internal IP of the local machine
func getLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			return ipNet.IP.String(), nil
		}
	}

	return "", fmt.Errorf("no local IP found")
}

// ListPortMappings lists the current port mappings from the router
func ListPortMappings(controlURL string, index int) error {
	soapBody := fmt.Sprintf(`<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
  <s:Body>
    <u:GetGenericPortMappingEntry xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">
      <NewPortMappingIndex>%d</NewPortMappingIndex>
    </u:GetGenericPortMappingEntry>
  </s:Body>
</s:Envelope>`, index)
	return sendSOAPRequest(controlURL, soapBody, "GetGenericPortMappingEntry")
}

// DeletePortMapping sends a SOAP request to delete a port mapping
func DeletePortMapping(controlURL string, externalPort int, protocol string) error {
	soapBody := fmt.Sprintf(`<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
  <s:Body>
    <u:DeletePortMapping xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">
      <NewRemoteHost></NewRemoteHost>
      <NewExternalPort>%d</NewExternalPort>
      <NewProtocol>%s</NewProtocol>
    </u:DeletePortMapping>
  </s:Body>
</s:Envelope>`, externalPort, protocol)

	return sendSOAPRequest(controlURL, soapBody, "DeletePortMapping")
}
func sendSOAPRequest(controlURL, soapBody, action string) error {
	req, err := http.NewRequest("POST", controlURL, bytes.NewBuffer([]byte(soapBody)))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	// Set SOAP action header and content type
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", fmt.Sprintf(`"urn:schemas-upnp-org:service:WANIPConnection:1#%s"`, action))

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	// Convert body to a string for checking the SOAP fault
	bodyStr := string(body)

	// Check if the response contains a SOAP Fault
	if strings.Contains(bodyStr, "<s:Fault>") {
		// Extract the error code and description from the response (example parsing, might need adjustments)
		faultCode := extractTagValue(bodyStr, "faultcode")
		errorCode := extractTagValue(bodyStr, "errorCode")
		errorDescription := extractTagValue(bodyStr, "errorDescription")

		return fmt.Errorf("SOAP Fault: faultCode=%s, errorCode=%s, errorDescription=%s", faultCode, errorCode, errorDescription)
	}

	// If no fault, print the response
	fmt.Printf("Response for %s:\n%s\n", action, bodyStr)
	return nil
}

// Helper function to extract value between specific XML tags
func extractTagValue(body, tag string) string {
	startTag := fmt.Sprintf("<%s>", tag)
	endTag := fmt.Sprintf("</%s>", tag)
	start := strings.Index(body, startTag)
	if start == -1 {
		return ""
	}
	start += len(startTag)
	end := strings.Index(body[start:], endTag)
	if end == -1 {
		return ""
	}
	return body[start : start+end]
}
