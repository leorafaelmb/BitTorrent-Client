package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"time"
)

type Peer struct {
	AddrPort *netip.AddrPort
	ID       [20]byte

	Conn   net.Conn
	Choked bool

	Bitfield BitField
}

type BitField []byte

type PeerMessage struct {
	Length  uint32
	ID      byte
	Payload []byte
}

func (p *Peer) Connect() error {
	conn, err := net.DialTimeout("tcp", p.AddrPort.String(), ConnectionTimeout*time.Second)
	if err != nil {
		return fmt.Errorf("error connecting to peer: %w", err)
	}
	p.Conn = conn
	return nil
}

type Handshake struct {
	PstrLen  byte
	Pstr     [19]byte
	Reserved [8]byte
	InfoHash [20]byte
	PeerID   [20]byte
}

func (p *Peer) Handshake(t TorrentFile, ext bool) (*Handshake, error) {
	c := p.Conn
	message, err := constructHandshakeMessage(t, ext)
	if err != nil {
		return nil, fmt.Errorf("error constructing peer handshake message: %w", err)
	}
	_, err = c.Write(message)
	if err != nil {
		return nil, fmt.Errorf("error writing peer handshake message to connection: %w", err)
	}
	h, err := readHandshake(p.Conn)
	if err != nil {
		return nil, err
	}
	if t.Info.InfoHash != h.InfoHash {
		return h, fmt.Errorf("handshake info hash does not match torrent info hash: %w", err)

	}

	copy(p.ID[:], h.PeerID[:])

	return h, nil
}

func constructHandshakeMessage(t TorrentFile, ext bool) ([]byte, error) {
	message := make([]byte, HandshakeLength)
	infoHash := t.Info.InfoHash

	message[0] = ProtocolStringLength
	copy(message[1:20], ProtocolString)
	copy(message[20:28], make([]byte, 8))
	copy(message[28:48], infoHash[:])
	copy(message[48:68], PeerID)

	if ext {
		message[25] = ExtensionID
	}

	return message, nil
}

func readHandshake(conn net.Conn) (*Handshake, error) {
	buf := make([]byte, 68)
	_, err := io.ReadFull(conn, buf)
	if err != nil {
		return nil, fmt.Errorf("error reading handshake response: %w", err)
	}

	h := &Handshake{}
	r := bytes.NewReader(buf)

	h.PstrLen, err = r.ReadByte()
	if err != nil {
		return nil, err
	}

	if _, err = io.ReadFull(r, h.Pstr[:]); err != nil {
		return nil, err
	}

	if _, err = io.ReadFull(r, h.Reserved[:]); err != nil {
		return nil, err
	}

	if _, err = io.ReadFull(r, h.InfoHash[:]); err != nil {
		return nil, err
	}

	if _, err = io.ReadFull(r, h.PeerID[:]); err != nil {
		return nil, err
	}

	// Validate handshake message
	if h.PstrLen != ProtocolStringLength || string(h.Pstr[:]) != ProtocolString {
		fmt.Println(string(h.Pstr[:]))
		err = fmt.Errorf("invalid handshake: %w", err)
	}
	return h, err
}

func (p *Peer) SendMessage(messageID byte, payload []byte) (*PeerMessage, error) {
	length := uint32(len(payload) + 1)
	message := make([]byte, 4+length)

	binary.BigEndian.PutUint32(message[0:4], length)
	message[4] = messageID
	copy(message[5:], payload)

	if _, err := p.Conn.Write(message); err != nil {
		return nil, err
	}

	response, err := p.ReadMessage()

	return response, err
}

func (p *Peer) ReadMessage() (*PeerMessage, error) {
	var err error
	lenBytes := make([]byte, 4)
	if _, err = io.ReadFull(p.Conn, lenBytes); err != nil {
		return nil, fmt.Errorf("error reading length of peer message: %w", err)
	}

	length := binary.BigEndian.Uint32(lenBytes)
	buf := make([]byte, length)
	r := bytes.NewReader(buf)

	_, err = io.ReadFull(p.Conn, buf)
	if err != nil {
		return nil, fmt.Errorf("error reading data stream into buffer: %w", err)
	}
	id, err := r.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("error reading message ID of peer message: %w", err)
	}

	payload := make([]byte, length-1)
	if _, err = io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("error reading payload of peer message: %w", err)
	}

	return &PeerMessage{
		Length:  length,
		ID:      id,
		Payload: payload,
	}, err

}

func (p *Peer) ReadBitfield() (*PeerMessage, error) {
	msg, err := p.ReadMessage()
	if err != nil {
		return msg, fmt.Errorf("failed to read bitfield: %w", err)
	}
	if msg.ID != MessageBitfield {
		return msg, fmt.Errorf("expected bitfield (5), got %d", msg.ID)
	}

	p.Bitfield = msg.Payload

	return msg, nil
}

func (p *Peer) SendInterested() (*PeerMessage, error) {
	return p.SendMessage(2, nil)
}

func (p *Peer) SendRequest(index, begin, block uint32) (*PeerMessage, error) {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], index)
	binary.BigEndian.PutUint32(payload[4:8], begin)
	binary.BigEndian.PutUint32(payload[8:12], block)

	return p.SendMessage(6, payload)
}

func (p *Peer) constructPieceRequest(index, begin, length uint32) []byte {
	request := make([]byte, 17)

	// Set message length
	binary.BigEndian.PutUint32(request[0:4], 13)

	// Set message ID
	request[4] = byte(6)

	// Set payload: index, begin, and length respectively
	binary.BigEndian.PutUint32(request[5:9], index)
	binary.BigEndian.PutUint32(request[9:13], begin)
	binary.BigEndian.PutUint32(request[13:17], length)

	return request

}

type BlockRequest struct {
	Index  uint32
	Begin  uint32
	Length uint32
}

func (p *Peer) sendRequestOnly(index, begin, length uint32) error {
	request := p.constructPieceRequest(index, begin, length)

	if _, err := p.Conn.Write(request); err != nil {
		return fmt.Errorf("error writing request to connection: %w", err)
	}

	return nil
}

func (p *Peer) getBlocks(requests []BlockRequest) ([][]byte, error) {
	numBlocks := len(requests)
	blocks := make([][]byte, numBlocks)

	requested := 0
	received := 0

	for received < numBlocks {
		for requested < numBlocks && requested-received < MaxPipelineRequests {
			req := requests[requested]

			if err := p.sendRequestOnly(req.Index, req.Begin, req.Length); err != nil {
				return nil, fmt.Errorf("error sending request for block %d: %w", requested, err)
			}
			requested++
		}
		msg, err := p.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("error reading message for block %d: %w", received, err)
		}
		if msg.ID != MessagePiece {
			return nil, fmt.Errorf("expected piece message (7), got %d", msg.ID)
		}

		// Extract block data (skip index and begin offset, which are first 8 bytes)
		if len(msg.Payload) < 8 {
			return nil, fmt.Errorf("piece message payload too short: %d bytes", len(msg.Payload))
		}

		blockData := msg.Payload[8:]
		blocks[received] = blockData
		received++
	}
	return blocks, nil
}

func (p *Peer) getPiece(pieceHash []byte, pieceLength, pieceIndex uint32) ([]byte, error) {
	piece := make([]byte, 0, pieceLength)

	var requests []BlockRequest
	var begin uint32 = 0
	remaining := pieceLength

	for remaining > 0 {
		blockLen := BlockSize
		if remaining < BlockSize {
			blockLen = remaining
		}

		requests = append(requests, BlockRequest{
			Index:  pieceIndex,
			Begin:  begin,
			Length: blockLen,
		})

		begin += blockLen
		remaining -= blockLen
	}

	blocks, err := p.getBlocks(requests)
	if err != nil {
		return nil, fmt.Errorf("error downloading blocks: %w", err)
	}

	for _, block := range blocks {
		piece = append(piece, block...)
	}

	if !bytes.Equal(hashPiece(piece), pieceHash) {
		return nil, fmt.Errorf("invalid piece hash for piece %d", pieceIndex)
	}

	return piece, nil
}

func (p *Peer) DownloadMetadata(magnet *MagnetLink) (*Info, error) {
	// Perform extension handshake
	extResp, err := p.ExtensionHandshake()
	if err != nil {
		return nil, fmt.Errorf("extension handshake failed: %w", err)
	}

	if extResp.MetadataSize == 0 {
		return nil, fmt.Errorf("peer reported metadata_size of 0")
	}

	numPieces := (extResp.MetadataSize + MetadataPieceSize - 1) / MetadataPieceSize

	fmt.Printf("Downloading metadata: %d bytes in %d pieces\n", extResp.MetadataSize, numPieces)

	// Download metadata pieces
	metadata := make([]byte, 0, extResp.MetadataSize)
	for i := 0; i < numPieces; i++ {
		fmt.Printf("Requesting metadata piece %d/%d\n", i+1, numPieces)

		piece, err := p.RequestMetadataPiece(byte(extResp.UtMetadataID), i)
		if err != nil {
			return nil, fmt.Errorf("failed to get metadata piece %d: %w", i, err)
		}

		metadata = append(metadata, piece.Data...)
	}

	// Trim to exact size
	if len(metadata) > extResp.MetadataSize {
		metadata = metadata[:extResp.MetadataSize]
	}

	// Verify info hash
	calculatedHash := hashPiece(metadata)
	if !bytes.Equal(calculatedHash, magnet.InfoHash[:]) {
		return nil, fmt.Errorf("metadata hash mismatch")
	}

	// Decode metadata (it's a bencoded info dict)
	decoded, err := Decode(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to decode metadata: %w", err)
	}

	infoDict, ok := decoded.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("metadata is not a dictionary")
	}

	return newInfo(infoDict)
}

func (p *Peer) ParseBitfield(msg *PeerMessage) error {
	if msg.ID != MessageBitfield {
		return fmt.Errorf("expected bitfield message (id 5), got id %d", msg.ID)
	}
	p.Bitfield = msg.Payload
	return nil
}

func (bf BitField) HasPiece(index int) bool {
	byteIndex := index / 8
	offset := index % 8
	if byteIndex < 0 || byteIndex >= len(bf) {
		return false
	}
	// Check if the bit is set (bits are ordered from most significant to least)
	return bf[byteIndex]>>(7-offset)&1 != 0
}
