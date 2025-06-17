package commonlib

import (
	"testing"
	"time"

	"github.com/NeuronInnovations/neuron-go-hedera-sdk/keylib"
	"github.com/NeuronInnovations/neuron-go-hedera-sdk/types"
	"github.com/hashgraph/hedera-sdk-go/v2"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
)

func TestGetBuffer(t *testing.T) {
	// Create a new NodeBuffers instance
	nb := NewNodeBuffers()

	// Convert the public key to peer.ID
	publicKey := "02c7370bf416ee6e9f9a430a12869c456d93db6b7392a9f90d0db8981190f47153"
	peerIDStr, err := keylib.ConvertHederaPublicKeyToPeerID(publicKey)
	if err != nil {
		t.Fatalf("Error converting public key to peer ID: %v", err)
	}
	peerID, err := peer.Decode(peerIDStr)
	if err != nil {
		t.Fatalf("Error decoding peer ID: %v", err)
	}

	// Create a test topic ID
	testTopicID := hedera.TopicID{
		Shard: 0,
		Realm: 0,
		Topic: 1234,
	}

	// Create a test buffer
	testBuffer := &NodeBufferInfo{
		LastOtherSideMultiAddress:      "test-address",
		LibP2PState:                    types.Connected,
		RendezvousState:                types.SendOK,
		IsOtherSideValidAccount:        true,
		NoOfConnectionAttempts:         0,
		LastConnectionAttempt:          time.Now(),
		NextScheduledConnectionAttempt: time.Now().Add(time.Hour),
		RequestOrResponse: types.TopicPostalEnvelope{
			Message:         "test-message",
			OtherStdInTopic: testTopicID,
		},
		NextScheduleRequestTime: time.Now(),
		LastGoodsReceivedTime:   time.Now(),
	}

	// Add the buffer to NodeBuffers
	nb.Buffers[peerID] = testBuffer

	// Test GetBuffer
	buffer, exists := nb.GetBuffer(peerID)

	// Verify results
	assert.True(t, exists, "Buffer should exist")
	assert.NotNil(t, buffer, "Buffer should not be nil")
	assert.Equal(t, testBuffer, buffer, "Retrieved buffer should match the added buffer")

	// Test with non-existent peer ID
	nonExistentPeerID, _ := peer.Decode("QmNonExistent")
	buffer, exists = nb.GetBuffer(nonExistentPeerID)
	assert.False(t, exists, "Buffer should not exist for non-existent peer ID")
	assert.Nil(t, buffer, "Buffer should be nil for non-existent peer ID")
}
