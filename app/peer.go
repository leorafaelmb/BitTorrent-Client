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

	BitField []byte
}

type Handshake struct {
	PstrLen  byte
	Pstr     [19]byte
	Reserved [8]byte
	InfoHash [20]byte
	PeerID   [20]byte
}

type PeerMessage struct {
	length  uint32
	id      byte
	payload []byte
}

func newPeer(hostport string) (*Peer, error) {
	addrPort, err := netip.ParseAddrPort(hostport)
	if err != nil {
		return nil, fmt.Errorf("error parsing hostport")
	}

	if !addrPort.IsValid() {
		return nil, fmt.Errorf("invalid address")
	}
	return &Peer{
		AddrPort: &addrPort,
	}, nil
}

func (p *Peer) Connect() error {
	conn, err := net.DialTimeout("tcp", p.AddrPort.String(), 3*time.Second)
	if err != nil {
		return fmt.Errorf("error connecting to peer: %w", err)
	}
	p.Conn = conn
	return nil
}

func (p *Peer) Handshake(t TorrentFile) (*Handshake, error) {
	c := p.Conn
	message, err := constructHandshakeMessage(t)
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
	if t.Info.getInfoHash() != h.InfoHash {
		return nil, fmt.Errorf("handshake info hash does not match torrent info hash: %w", err)

	}

	copy(p.ID[:], h.PeerID[:])

	return h, nil

}

func (p *Peer) SendMessage(messageID byte, payload []byte) (*PeerMessage, error) {
	length := uint32(len(payload) + 1)
	message := make([]byte, 4+length)

	binary.BigEndian.PutUint32(message[0:4], length)
	message[4] = messageID
	message = append(message, payload...)

	if _, err := p.Conn.Write(message); err != nil {
		return nil, err
	}

	response, err := p.ReadMessage()

	return response, err

}

func (p *Peer) ReadMessage() (*PeerMessage, error) {
	var err error
	m := &PeerMessage{}

	lenBytes := make([]byte, 4)
	if _, err = io.ReadFull(p.Conn, lenBytes); err != nil {
		return nil, fmt.Errorf("error reading length of peer message: %w", err)
	}
	length := binary.BigEndian.Uint32(lenBytes)
	m.length = length

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
	m.id = id

	payload := make([]byte, length-1)
	if _, err = io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("error reading payload of peer message: %w", err)
	}

	m.payload = payload

	if id < 0 || id > 8 {
		err = fmt.Errorf("invalid message ID: %w", err)
	}

	return &PeerMessage{
		length:  length,
		id:      id,
		payload: payload,
	}, err

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

func (p *Peer) getBlock(index, begin, length uint32) ([]byte, error) {
	request := p.constructPieceRequest(index, begin, length)
	if _, err := p.Conn.Write(request); err != nil {
		return nil, err
	}

	m, err := p.ReadMessage()
	if err != nil {
		return nil, err
	}

	return m.payload[8:], nil
}

func (p *Peer) getPiece(pieceHash []byte, pieceLength, pieceIndex uint32) ([]byte, error) {
	piece := make([]byte, 0, pieceLength)

	var blockLen uint32 = 1 << 14
	var begin uint32 = 0

	for pieceLength > 0 {
		if pieceLength < 1<<14 {
			blockLen = pieceLength
		}

		block, err := p.getBlock(pieceIndex, begin, blockLen)
		if err != nil {
			return nil,
				fmt.Errorf("error getting piece %d at byte offset %d with length %d: %w\n",
					pieceIndex, begin, blockLen, err)
		}

		piece = append(piece, block...)
		begin += blockLen
		pieceLength -= blockLen
	}

	validated := validatePiece(piece, pieceHash)
	if !validated {
		return nil, fmt.Errorf("piece hash not validated")
	}

	return piece, nil
}
