package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

type MagnetLink struct {
	TrackerURL  string
	InfoHash    [20]byte
	HexInfoHash string
}

func DeserializeMagnet(uri string) (*MagnetLink, error) {
	magnetUri, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}

	trackerURL := magnetUri.Query()["tr"][0]
	hexInfoHash := strings.ReplaceAll(magnetUri.Query()["xt"][0], "urn:btih:", "")

	var infoHash [20]byte
	decodedHash, err := hex.DecodeString(hexInfoHash)
	if err != nil {
		return nil, err
	}
	copy(infoHash[:], decodedHash)

	return &MagnetLink{
		TrackerURL:  trackerURL,
		InfoHash:    infoHash,
		HexInfoHash: hexInfoHash,
	}, nil
}

func (p *Peer) ExtensionHandshake() (*ExtensionHandshakeResponse, error) {
	payload := []byte{0}
	payload = append(payload, []byte("d1:md11:ut_metadatai1eee")...)

	// Message ID 20 for extension protocol
	msg, err := p.SendMessage(20, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to send extension handshake: %w", err)
	}

	if msg.ID != 20 {
		return nil, fmt.Errorf("expected extension message (20), got %d", msg.ID)
	}

	return parseExtensionHandshake(msg.Payload)
}

type ExtensionHandshakeResponse struct {
	MetadataSize     int
	UtMetadataID     int
	ExtensionMapping map[string]int
}

func parseExtensionHandshake(payload []byte) (*ExtensionHandshakeResponse, error) {
	decoded, err := Decode(payload[1:])
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

func (p *Peer) MagnetHandshake(infoHash [20]byte) (*Handshake, error) {
	c := p.Conn
	message := constructMagnetHandshakeMessage(infoHash)

	_, err := c.Write(message)
	if err != nil {
		return nil, fmt.Errorf("error writing magnet handshake message: %w", err)
	}

	h, err := readHandshake(p.Conn)
	if err != nil {
		return nil, err
	}

	if !bytes.Equal(infoHash[:], h.InfoHash[:]) {
		return nil, fmt.Errorf("handshake info hash mismatch")
	}

	copy(p.ID[:], h.PeerID[:])

	// Check if peer supports extension protocol (bit 20 in reserved bytes)
	if h.Reserved[5]&0x10 == 0 {
		return nil, fmt.Errorf("peer does not support extension protocol")
	}

	return h, nil
}

func constructMagnetHandshakeMessage(infoHash [20]byte) []byte {
	message := make([]byte, 68)

	message[0] = byte(19)
	copy(message[1:20], "BitTorrent protocol")

	// Set bit 20 to indicate extension support (byte 5, bit 4)
	reserved := make([]byte, 8)
	reserved[5] = 0x10
	copy(message[20:28], reserved)

	copy(message[28:48], infoHash[:])
	copy(message[48:68], "leofeopeoluvsanayeli")

	return message
}
