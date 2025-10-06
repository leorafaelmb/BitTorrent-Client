package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"time"
)

type Peer struct {
	IP   net.IP
	Port uint16
	ID   [20]byte

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

func (p *Peer) Connect() error {
	conn, err := net.DialTimeout("tcp", p.IP.String(), 3*time.Second)
	if err != nil {
		return err
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
		return nil, fmt.Errorf("handshake info hash does not match torrent info hash")

	}

	copy(p.ID[:], h.PeerID[:])

	return h, nil

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

	if h.PstrLen != 68 || string(h.Pstr[:]) != "BitTorrent Protocol" {
		return nil, err
	}
	return h, nil
}

type PeerMessage struct {
	length  uint32
	id      int
	payload []byte
}

func newPeerMessage(length uint32, id int, payload []byte) *PeerMessage {
	return &PeerMessage{
		length:  length,
		id:      id,
		payload: payload,
	}
}

func readPeerMessage(conn net.Conn) (*PeerMessage, error) {
	var err error

	lenBytes := make([]byte, 4)
	if _, err = io.ReadFull(conn, lenBytes); err != nil {
		return nil, fmt.Errorf("error reading length of peer message: %w", err)
	}
	length := binary.BigEndian.Uint32(lenBytes)

	id := make([]byte, 1)
	if _, err = io.ReadFull(conn, id); err != nil {
		return nil, fmt.Errorf("error reading message ID of peer message: %w", err)
	}
	payload := make([]byte, length-1)
	if _, err = io.ReadFull(conn, payload); err != nil {
		return nil, fmt.Errorf("error reading payload of peer message: %w", err)
	}

	return newPeerMessage(length, int(id[0]), payload), nil
}

func constructHandshakeMessage(t TorrentFile) ([]byte, error) {
	var message []byte
	infoHash := t.Info.getInfoHash()
	message = append(message, byte(19))
	message = append(message, []byte("BitTorrent protocol")...)
	message = append(message, make([]byte, 8)...)
	message = append(message, infoHash[:]...)
	peerId := make([]byte, 20)
	if _, err := rand.Read(peerId); err != nil {
		return nil, fmt.Errorf("error constructing random 20-byte byte slice: %w", err)
	}
	message = append(message, peerId...)
	return message, nil
}

func handshake(conn net.Conn, t TorrentFile) ([]byte, error) {
	message, err := constructHandshakeMessage(t)
	if err != nil {
		return nil, fmt.Errorf("error constructing peer handshake message: %w", err)
	}
	_, err = conn.Write(message)
	if err != nil {
		return nil, fmt.Errorf("error writing peer handshake message to connection: %w", err)

	}
	respBytes := make([]byte, 68)
	_, err = conn.Read(respBytes)

	if err != nil {
		return nil, fmt.Errorf("error reading peer handshake response: %w", err)
	}
	return respBytes, nil

}

func constructPieceRequest(index, begin, length uint32) []byte {
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

func getBlock(conn net.Conn, index, begin, length uint32) ([]byte, error) {
	request := constructPieceRequest(index, begin, length)
	if _, err := conn.Write(request); err != nil {
		return nil, err
	}

	m, err := readPeerMessage(conn)
	if err != nil {
		return nil, err
	}

	return m.payload[8:], nil
}

func getPiece(conn net.Conn, pieceHash []byte, pieceLength, pieceIndex uint32) ([]byte, error) {
	piece := make([]byte, 0, pieceLength)

	var blockLen uint32 = 1 << 14
	var begin uint32 = 0

	for pieceLength > 0 {
		if pieceLength < 1<<14 {
			blockLen = pieceLength
		}

		block, err := getBlock(conn, pieceIndex, begin, blockLen)
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

func hashPiece(piece []byte) []byte {
	hasher := sha1.New()
	hasher.Write(piece)
	sha := hasher.Sum(nil)
	return sha
}
func validatePiece(piece, pieceHash []byte) bool {
	return fmt.Sprintf("%x", hashPiece(piece)) == fmt.Sprintf("%x", pieceHash)
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	command := os.Args[1]

	switch command {
	case "decode":
		bencodedValue := os.Args[2]

		decoded, _, err := decode([]byte(bencodedValue), 0)
		if err != nil {
			return err
		}
		jsonOutput, err := json.Marshal(decoded)
		fmt.Println(string(jsonOutput))

	case "info":
		filePath := os.Args[2]
		t, err := newTorrentFileFromFilePath(filePath)
		if err != nil {
			return err
		}
		fmt.Println(t)
	case "peers":
		filePath := os.Args[2]
		t, err := newTorrentFileFromFilePath(filePath)
		if err != nil {
			return err
		}

		var (
			trackerURL = t.Announce
			infoHash   = urlEncodeInfoHash(t.Info.getHexInfoHash())
			peerId     = "leofeopeoluvsanayeli"
			left       = t.Info.length
		)

		r := newTrackerRequest(trackerURL, infoHash, peerId, left)
		body, err := r.SendRequest()
		if err != nil {
			return err
		}

		tres, err := newTrackerResponseFromBytes(body)
		if err != nil {
			return err
		}

		fmt.Println(tres.PeersString())

	case "handshake":
		filePath := os.Args[2]
		peerAddress := os.Args[3]
		t, err := newTorrentFileFromFilePath(filePath)
		if err != nil {
			return err
		}
		conn, err := net.Dial("tcp", peerAddress)
		if err != nil {
			return fmt.Errorf("error opening TCP connection to peer: %w", err)
		}
		defer conn.Close()

		response, err := handshake(conn, *t)
		if err != nil {
			return err
		}

		peerResponseId := fmt.Sprintf("Peer ID: %x", response[48:])
		fmt.Println(peerResponseId)
	case "download_piece":
		downloadFilePath := os.Args[3]
		torrentFilePath := os.Args[4]
		pieceIndex, err := strconv.Atoi(os.Args[5])
		if err != nil {
			return err
		}

		t, err := newTorrentFileFromFilePath(torrentFilePath)
		if err != nil {
			return err
		}

		var (
			trackerURL = t.Announce
			infoHash   = urlEncodeInfoHash(t.Info.getHexInfoHash())
			peerId     = "leofeopeoluvsanayeli"
			left       = t.Info.length
		)

		treq := newTrackerRequest(trackerURL, infoHash, peerId, left)
		body, err := treq.SendRequest()
		if err != nil {
			return err
		}
		tres, err := newTrackerResponseFromBytes(body)
		if err != nil {
			return err
		}

		peers := tres.getPeers()
		conn, err := net.Dial("tcp", peers[0])
		if err != nil {
			return err
		}
		defer conn.Close()

		_, err = handshake(conn, *t)
		if err != nil {
			return err
		}

		// bitfield
		peerMessage, err := readPeerMessage(conn)
		if err != nil {
			return err
		}

		if peerMessage.id != 5 {
			return fmt.Errorf("incorrect message id: expected 5 got %d", peerMessage.id)
		}

		// interested msg
		if _, err = conn.Write([]byte{0, 0, 0, 1, 2}); err != nil {
			return err
		}

		// unchoke
		peerMessage, err = readPeerMessage(conn)
		if peerMessage.id != 1 {
			return fmt.Errorf("incorrect message id: expected 1 got %d", peerMessage.id)
		}
		pieceLength := uint32(t.Info.pieceLength)
		pieceHash := t.Info.pieces[pieceIndex : 20+pieceIndex]

		if pieceIndex == len(t.Info.pieces)/20-1 {
			pieceLength = uint32(t.Info.length) - pieceLength*uint32(len(t.Info.pieces)/20-1)
		}

		piece, err := getPiece(conn, pieceHash, pieceLength, uint32(pieceIndex))
		if err != nil {
			return err
		}

		f, err := os.Create(downloadFilePath)
		if err != nil {
			return err
		}

		defer f.Close()
		if _, err = f.Write(piece); err != nil {
			return err
		}

	default:
		return fmt.Errorf("unknown command: %s", command)
	}
	return nil
}
