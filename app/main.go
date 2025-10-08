package main

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"log"
	"net/netip"
	"os"
	"strconv"
)

func hashPiece(piece []byte) []byte {
	hasher := sha1.New()
	hasher.Write(piece)
	sha := hasher.Sum(nil)
	return sha
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

		addrPort, err := netip.ParseAddrPort(peerAddress)

		p := Peer{
			AddrPort: &addrPort,
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

		peers := trackerResponse.Peers

		p := Peer{
			AddrPort: &peers[0],
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
