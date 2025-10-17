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

func ConnectToMagnetPeer(magnetURL string) (*Peer, *MagnetLink, error) {
	magnet, err := DeserializeMagnet(magnetURL)
	if err != nil {
		return nil, nil, err
	}

	treq := newTrackerRequest(magnet.TrackerURL,
		urlEncodeInfoHash(magnet.HexInfoHash), 999)

	tres, err := treq.SendRequest()
	if err != nil {
		return nil, nil, err
	}

	p := &Peer{AddrPort: &tres.Peers[0]}
	if err = p.Connect(); err != nil {
		return nil, nil, err
	}

	if _, err = p.MagnetHandshake(magnet.InfoHash); err != nil {
		p.Conn.Close()
		return nil, nil, err
	}

	if _, err = p.ReadBitfield(); err != nil {
		p.Conn.Close()
		return nil, nil, err
	}

	return p, magnet, nil
}

func (p *Peer) ExtensionHandshake() (*ExtensionHandshakeResponse, error) {
	payload := append([]byte{0}, []byte("d1:md11:ut_metadatai1eee")...)

	// Message ID 20 for extension protocol
	msg, err := p.SendMessage(20, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to send extension handshake: %w", err)
	}

	if msg.ID != MessageExtension {
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

	// Check if peer supports extension protocol
	if h.Reserved[ExtensionBitPosition]&ExtensionID == 0 {
		return nil, fmt.Errorf("peer does not support extension protocol")
	}

	return h, nil
}

func constructMagnetHandshakeMessage(infoHash [20]byte) []byte {
	message := make([]byte, HandshakeLength)

	message[0] = byte(ProtocolStringLength)
	copy(message[1:20], ProtocolString)

	// Indicate extension support
	reserved := make([]byte, 8)
	reserved[ExtensionBitPosition] = ExtensionID
	copy(message[20:28], reserved)

	copy(message[28:48], infoHash[:])
	copy(message[48:68], PeerID)

	return message
}

// RequestMetadataPiece requests a piece of the metadata
func (p *Peer) RequestMetadataPiece(utMetadataID byte, piece int) (*MetadataPiece, error) {
	// Build request message
	request := fmt.Sprintf("d8:msg_typei0e5:piecei%dee", piece)

	payload := append([]byte{utMetadataID}, []byte(request)...)

	msg, err := p.SendMessage(20, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to send metadata request: %w", err)
	}

	if msg.ID != MessageExtension {
		return nil, fmt.Errorf("expected extension message (20), got %d", msg.ID)
	}

	return parseMetadataPiece(msg.Payload)
}

type MetadataPiece struct {
	Piece     int
	TotalSize int
	Data      []byte
}

func parseMetadataPiece(payload []byte) (*MetadataPiece, error) {
	if len(payload) < 2 {
		return nil, fmt.Errorf("metadata response too short")
	}

	// First byte is extension message ID, skip
	bencodedPart := payload[1:]
	decoded, dictEnd, err := decodeBencode(bencodedPart, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to decode metadata response: %w", err)
	}
	dict, ok := decoded.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("metadata response not a dictionary")
	}
	// Check msg_type (should be 1 for data)
	msgType, ok := dict["msg_type"].(int)
	if !ok || msgType != 1 {
		return nil, fmt.Errorf("invalid msg_type in metadata response")
	}

	piece, ok := dict["piece"].(int)
	if !ok {
		return nil, fmt.Errorf("no piece index in metadata response")
	}

	totalSize, ok := dict["total_size"].(int)
	if !ok {
		return nil, fmt.Errorf("no total_size in metadata response")
	}

	// Extract the actual metadata data (everything after the bencoded dict)
	data := bencodedPart[dictEnd:]

	return &MetadataPiece{
		Piece:     piece,
		TotalSize: totalSize,
		Data:      data,
	}, nil
}
