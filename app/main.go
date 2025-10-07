package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
)

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
	if h.PstrLen != 19 || string(h.Pstr[:]) != "BitTorrent protocol" {
		return nil, fmt.Errorf("invalid handshake")
	}
	return h, nil
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

func hashPiece(piece []byte) []byte {
	hasher := sha1.New()
	hasher.Write(piece)
	sha := hasher.Sum(nil)
	return sha
}
func validatePiece(piece, pieceHash []byte) bool {
	return bytes.Equal(hashPiece(piece), pieceHash)
	//return fmt.Sprintf("%x", hashPiece(piece)) == fmt.Sprintf("%x", pieceHash)
}

func downloadFile(conn net.Conn, info Info) {

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
		trackerResponse, err := r.SendRequest()
		if err != nil {
			return err
		}

		fmt.Println(trackerResponse.PeersString())

	case "handshake":
		filePath := os.Args[2]
		peerAddress := os.Args[3]
		p, err := newPeer(peerAddress)
		if err != nil {
			return err
		}

		t, err := newTorrentFileFromFilePath(filePath)
		if err != nil {
			return err
		}
		err = p.Connect()
		if err != nil {
			return err
		}
		defer p.Conn.Close()

		response, err := p.Handshake(*t)
		if err != nil {
			return err
		}

		peerResponseId := fmt.Sprintf("Peer ID: %x", response.PeerID)
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
		trackerResponse, err := treq.SendRequest()
		if err != nil {
			return err
		}

		peers := trackerResponse.getPeers()
		p, err := newPeer(peers[0])
		if err != nil {
			return err
		}
		if err = p.Connect(); err != nil {
			return err
		}
		defer p.Conn.Close()

		_, err = p.Handshake(*t)
		if err != nil {
			return err
		}

		// bitfield
		message, err := p.ReadMessage()

		if message.id != 5 {
			return fmt.Errorf("incorrect message id: expected 5 got %d", message.id)
		}

		// interested msg
		message, err = p.SendInterested()
		if err != nil {
			return err
		}
		// unchoke
		if message.id != 1 {
			return fmt.Errorf("incorrect message id: expected 1 got %d", message.id)
		}
		pieceLength := uint32(t.Info.pieceLength)
		pieceHash := t.Info.pieces[pieceIndex : 20+pieceIndex]

		if pieceIndex == len(t.Info.pieces)/20-1 {
			pieceLength = uint32(t.Info.length) - pieceLength*uint32(len(t.Info.pieces)/20-1)
		}

		piece, err := p.getPiece(pieceHash, pieceLength, uint32(pieceIndex))
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
	case "download":

	default:
		return fmt.Errorf("unknown command: %s", command)
	}
	return nil
}
