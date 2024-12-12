package upnp

import (
	"testing"
)

func TestListPortMappings(t *testing.T) {
	type args struct {
		controlURL string
		index      int
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"TestListPortMappings", args{"http://192.168.0.1:5000/rootDesc.xml", 2}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			if err := ListPortMappings(tt.args.controlURL, tt.args.index); (err != nil) != tt.wantErr {
				t.Errorf("ListPortMappings() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSendSSDPRequest(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		wantErr bool
	}{
		{"TestSendSSDPRequest", "http://192.168.0.1:5000/rootDesc.xml", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SendSSDPRequest()
			if (err != nil) != tt.wantErr {
				t.Errorf("SendSSDPRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SendSSDPRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddPortMapping(t *testing.T) {
	type args struct {
		controlURL   string
		externalPort int
		internalPort int
		protocol     string
		description  string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{

		{"TestAddPortMapping", args{"http://192.168.0.1:5000/rootDesc.xml", 1701, 1701, "udp", "neuron-buyer-ports"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := AddPortMapping(tt.args.controlURL, tt.args.externalPort, tt.args.internalPort, tt.args.protocol, tt.args.description); (err != nil) != tt.wantErr {
				t.Errorf("AddPortMapping() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
