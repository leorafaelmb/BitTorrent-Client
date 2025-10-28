package peer

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/codecrafters-io/bittorrent-starter-go/internal"
	"github.com/codecrafters-io/bittorrent-starter-go/internal/bencode"
	"github.com/codecrafters-io/bittorrent-starter-go/internal/metainfo"
	"io"
	"net"
	"net/netip"
	"time"
)

// Peer represents a network connection to another BitTorrent client.
type Peer struct {
	AddrPort *netip.AddrPort
	ID       [20]byte

	Conn   net.Conn
	Choked bool

	Bitfield BitField
}

// BitField is a compact representation of which pieces a peer has.
type BitField []byte

// PeerMessage represents a message sent between peers after the handshake
type PeerMessage struct {
	Length  uint32
	ID      byte
	Payload []byte
}

// Connect establishes a TCP connection to the peer
func (p *Peer) Connect() error {
	conn, err := net.DialTimeout("tcp", p.AddrPort.String(), internal.ConnectionTimeout*time.Second)
	if err != nil {
		return fmt.Errorf("error connecting to peer: %w", err)
	}
	p.Conn = conn
	return nil
}

// Handshake performs the BitTorrent handshake with a peer.
func (p *Peer) Handshake(infoHash [20]byte, ext bool) (*Handshake, error) {
	c := p.Conn
	message, err := constructHandshakeMessage(infoHash, ext)
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
	if infoHash != h.InfoHash {
		return h, fmt.Errorf("handshake info hash does not match torrent info hash: %w", err)

	}

	copy(p.ID[:], h.PeerID[:])

	return h, nil
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
	if h.Reserved[internal.ExtensionBitPosition]&internal.ExtensionID == 0 {
		return nil, fmt.Errorf("peer does not support extension protocol")
	}

	return h, nil
}

func (p *Peer) ExtensionHandshake() (*ExtensionHandshakeResponse, error) {
	payload := append([]byte{0}, []byte("d1:md11:ut_metadatai1eee")...)

	// Message ID 20 for extension protocol
	msg, err := p.SendMessage(20, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to send extension handshake: %w", err)
	}

	if msg.ID != internal.MessageExtension {
		return nil, fmt.Errorf("expected extension message (20), got %d", msg.ID)
	}

	return parseExtensionHandshake(msg.Payload)
}

// readHandshake reads and parses a handshake message from the connection
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
	if h.PstrLen != internal.ProtocolStringLength || string(h.Pstr[:]) != internal.ProtocolString {
		fmt.Println(string(h.Pstr[:]))
		err = fmt.Errorf("invalid handshake: %w", err)
	}
	return h, err
}

// SendMessage sends a message to the peer and waits for a response.
// Used for messages that expect an immediate reply.
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

// ReadMessage reads one complete message from the peer.
// Blocks until a full message is received.
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

// ReadBitfield reads and stores the peer's bitfield message.
func (p *Peer) ReadBitfield() (*PeerMessage, error) {
	msg, err := p.ReadMessage()
	if err != nil {
		return msg, fmt.Errorf("failed to read bitfield: %w", err)
	}
	if msg.ID != internal.MessageBitfield {
		return msg, fmt.Errorf("expected bitfield (5), got %d", msg.ID)
	}

	p.Bitfield = msg.Payload

	return msg, nil
}

// SendInterested sends a message to the peer communicating we're interested in downloading from them
func (p *Peer) SendInterested() (*PeerMessage, error) {
	return p.SendMessage(2, nil)
}

// SendRequest requests a specific block from a piece.
// index: which piece, begin: byte offset within piece, block: number of bytes
func (p *Peer) SendRequest(index, begin, block uint32) (*PeerMessage, error) {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], index)
	binary.BigEndian.PutUint32(payload[4:8], begin)
	binary.BigEndian.PutUint32(payload[8:12], block)

	return p.SendMessage(6, payload)
}

// constructPieceRequest builds a request message
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

// BlockRequest represents a single block request within a piece
type BlockRequest struct {
	Index  uint32
	Begin  uint32
	Length uint32
}

// sendRequestOnly sends a request without waiting for a response.
// Used in pipelining to send multiple requests back-to-back.
func (p *Peer) sendRequestOnly(index, begin, length uint32) error {
	request := p.constructPieceRequest(index, begin, length)

	if _, err := p.Conn.Write(request); err != nil {
		return fmt.Errorf("error writing request to connection: %w", err)
	}

	return nil
}

// getBlocks downloads multiple blocks using TCP pipelining.
// Pipelining allows us to send up to MaxPipelineRequests without waiting,
// keeping the connection busy and dramatically improving download speed.
func (p *Peer) getBlocks(requests []BlockRequest) ([][]byte, error) {
	numBlocks := len(requests)
	blocks := make([][]byte, numBlocks)

	requested := 0
	received := 0

	for received < numBlocks {
		for requested < numBlocks && requested-received < internal.MaxPipelineRequests {
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
		if msg.ID != internal.MessagePiece {
			return nil, fmt.Errorf("expected piece message (7), got %d", msg.ID)
		}

		if len(msg.Payload) < 8 {
			return nil, fmt.Errorf("piece message payload too short: %d bytes", len(msg.Payload))
		}

		blockData := msg.Payload[8:]
		blocks[received] = blockData
		received++
	}
	return blocks, nil
}

// GetPiece downloads and verifies a complete piece.
// Breaks the piece into 16KB blocks and uses pipelining for download efficiency.
func (p *Peer) GetPiece(pieceHash []byte, pieceLength, pieceIndex uint32) ([]byte, error) {
	piece := make([]byte, 0, pieceLength)

	var requests []BlockRequest
	var begin uint32 = 0
	remaining := pieceLength

	for remaining > 0 {
		blockLen := internal.BlockSize
		if remaining < internal.BlockSize {
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

	if !bytes.Equal(metainfo.HashPiece(piece), pieceHash) {
		return nil, fmt.Errorf("invalid piece hash for piece %d", pieceIndex)
	}

	return piece, nil
}

// RequestMetadataPiece requests a piece of the metadata
func (p *Peer) RequestMetadataPiece(utMetadataID byte, piece int) (*metainfo.MetadataPiece, error) {
	// Build request message
	request := fmt.Sprintf("d8:msg_typei0e5:piecei%dee", piece)

	payload := append([]byte{utMetadataID}, []byte(request)...)

	msg, err := p.SendMessage(20, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to send metadata request: %w", err)
	}

	if msg.ID != internal.MessageExtension {
		return nil, fmt.Errorf("expected extension message (20), got %d", msg.ID)
	}

	return metainfo.ParseMetadataPiece(msg.Payload)
}

func (p *Peer) DownloadMetadata(magnet *metainfo.MagnetLink) (*metainfo.Info, error) {
	// Perform extension handshake
	extResp, err := p.ExtensionHandshake()
	if err != nil {
		return nil, fmt.Errorf("extension handshake failed: %w", err)
	}

	if extResp.MetadataSize == 0 {
		return nil, fmt.Errorf("peer reported metadata_size of 0")
	}

	numPieces := (extResp.MetadataSize + internal.MetadataPieceSize - 1) / internal.MetadataPieceSize

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
	calculatedHash := metainfo.HashPiece(metadata)
	if !bytes.Equal(calculatedHash, magnet.InfoHash[:]) {
		return nil, fmt.Errorf("metadata hash mismatch")
	}

	// Decode metadata (it's a bencoded info dict)
	decoded, err := bencode.Decode(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to decode metadata: %w", err)
	}

	infoDict, ok := decoded.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("metadata is not a dictionary")
	}

	return metainfo.NewInfo(infoDict)
}

func (p *Peer) ParseBitfield(msg *PeerMessage) error {
	if msg.ID != internal.MessageBitfield {
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
