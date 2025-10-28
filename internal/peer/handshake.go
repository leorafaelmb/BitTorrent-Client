package peer

import (
	"fmt"
	"github.com/codecrafters-io/bittorrent-starter-go/internal"
	"github.com/codecrafters-io/bittorrent-starter-go/internal/bencode"
)

// Handshake represents the first message exchanged between peers.
type Handshake struct {
	PstrLen  byte
	Pstr     [19]byte
	Reserved [8]byte
	InfoHash [20]byte
	PeerID   [20]byte
}

// constructHandshakeMessage creates the handshake message bytes.
func constructHandshakeMessage(infoHash [20]byte, ext bool) ([]byte, error) {
	message := make([]byte, internal.HandshakeLength)

	message[0] = internal.ProtocolStringLength
	copy(message[1:20], internal.ProtocolString)
	copy(message[20:28], make([]byte, 8))
	copy(message[28:48], infoHash[:])
	copy(message[48:68], internal.PeerID)

	if ext {
		message[25] = internal.ExtensionID
	}

	return message, nil
}

func constructMagnetHandshakeMessage(infoHash [20]byte) []byte {
	message := make([]byte, internal.HandshakeLength)

	message[0] = byte(internal.ProtocolStringLength)
	copy(message[1:20], internal.ProtocolString)

	// Indicate extension support
	reserved := make([]byte, 8)
	reserved[internal.ExtensionBitPosition] = internal.ExtensionID
	copy(message[20:28], reserved)

	copy(message[28:48], infoHash[:])
	copy(message[48:68], internal.PeerID)

	return message
}

type ExtensionHandshakeResponse struct {
	MetadataSize     int
	UtMetadataID     int
	ExtensionMapping map[string]int
}

func parseExtensionHandshake(payload []byte) (*ExtensionHandshakeResponse, error) {
	decoded, err := bencode.Decode(payload[1:])
	if err != nil {
		return nil, fmt.Errorf("failed to decode extension handshake: %w", err)
	}

	dict, ok := decoded.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("extension handshake not a dictionary")
	}

	response := &ExtensionHandshakeResponse{
		ExtensionMapping: make(map[string]int),
	}

	if metadataSize, ok := dict["metadata_size"].(int); ok {
		response.MetadataSize = metadataSize
	}

	if m, ok := dict["m"].(map[string]interface{}); ok {
		for key, val := range m {
			if id, ok := val.(int); ok {
				response.ExtensionMapping[key] = id
				if key == "ut_metadata" {
					response.UtMetadataID = id
				}
			}
		}
	}

	if response.UtMetadataID == 0 {
		return nil, fmt.Errorf("peer does not support ut_metadata extension")
	}

	return response, nil
}
