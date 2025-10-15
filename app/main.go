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

		decoded, err := Decode([]byte(bencodedValue))
		if err != nil {
			return err
		}
		jsonOutput, err := json.Marshal(decoded)
		fmt.Println(string(jsonOutput))

	case "info":
		filePath := os.Args[2]
		t, err := DeserializeTorrent(filePath)
		if err != nil {
			return err
		}
		fmt.Println(t)
	case "peers":
		filePath := os.Args[2]
		t, err := DeserializeTorrent(filePath)
		if err != nil {
			return err
		}

		var (
			trackerURL = t.Announce
			infoHash   = urlEncodeInfoHash(t.Info.getHexInfoHash())
			peerId     = "leofeopeoluvsanayeli"
			left       = t.Info.Length
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

		t, err := DeserializeTorrent(filePath)
		if err != nil {
			return err
		}
		err = p.Connect()
		if err != nil {
			return err
		}
		defer p.Conn.Close()

		response, err := p.Handshake(*t, false)
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

		t, err := DeserializeTorrent(torrentFilePath)
		if err != nil {
			return err
		}

		var (
			trackerURL = t.Announce
			infoHash   = urlEncodeInfoHash(t.Info.getHexInfoHash())
			peerId     = "leofeopeoluvsanayeli"
			left       = t.Info.Length
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

		_, err = p.Handshake(*t, false)
		if err != nil {
			return err
		}

		// bitfield
		message, err := p.ReadMessage()

		if message.ID != 5 {
			return fmt.Errorf("incorrect message id: expected 5 got %d", message.ID)
		}

		// interested msg
		message, err = p.SendInterested()
		if err != nil {
			return err
		}
		// unchoke
		if message.ID != 1 {
			return fmt.Errorf("incorrect message id: expected 1 got %d", message.ID)
		}

		pieceLength := uint32(t.Info.PieceLength)
		pieceHash := t.Info.pieceHashes()[pieceIndex]

		if pieceIndex == len(t.Info.Pieces)/20-1 {
			pieceLength = uint32(t.Info.Length) - pieceLength*uint32(len(t.Info.Pieces)/20-1)
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
		downloadFilePath := os.Args[3]
		torrentFilePath := os.Args[4]

		t, err := DeserializeTorrent(torrentFilePath)
		if err != nil {
			return err
		}

		var (
			trackerURL = t.Announce
			infoHash   = urlEncodeInfoHash(t.Info.getHexInfoHash())
			peerId     = "leofeopeoluvsanayeli"
			left       = t.Info.Length
		)

		treq := newTrackerRequest(trackerURL, infoHash, peerId, left)
		trackerResponse, err := treq.SendRequest()
		if err != nil {
			return err
		}

		peers := trackerResponse.Peers

		// Create Peer objects from addresses
		peerList := make([]Peer, len(peers))
		for i, addr := range peers {
			peerList[i] = Peer{AddrPort: &addr}
		}

		// Download using 5 concurrent workers
		fileBytes, err := t.DownloadFile(peerList, 5)
		if err != nil {
			return err
		}

		f, err := os.Create(downloadFilePath)
		if err != nil {
			return err
		}
		defer f.Close()

		if _, err = f.Write(fileBytes); err != nil {
			return err
		}
	case "magnet_parse":
		magnet, err := DeserializeMagnet(os.Args[2])
		if err != nil {
			return err
		}

		fmt.Println("Tracker URL:", magnet.TrackerURL)
		fmt.Println("Info Hash:", magnet.HexInfoHash)
	case "magnet_handshake":
		magnetUrl := os.Args[2]

		magnet, err := DeserializeMagnet(magnetUrl)
		//t := TorrentFile{
		//	Announce: magnet.TrackerURL,
		//	Info: &Info{
		//		Length:      999,
		//		PieceLength: 0,
		//		InfoHash:    magnet.InfoHash,
		//	}}
		treq := newTrackerRequest(magnet.TrackerURL, urlEncodeInfoHash(magnet.HexInfoHash),
			"leofeopeoluvsanayeli", 999)
		tres, err := treq.SendRequest()
		if err != nil {
			return err
		}

		p := Peer{AddrPort: &tres.Peers[0]}
		err = p.Connect()
		if err != nil {
			return err
		}
		defer p.Conn.Close()
		_, err = p.MagnetHandshake(magnet.InfoHash)
		if err != nil {
			return err
		}
		_, err = p.ReadBitfield()
		if err != nil {
			return err
		}

		_, err = p.ExtensionHandshake()
		if err != nil {
			return fmt.Errorf("extension handshake failed: %w", err)
		}

		fmt.Printf("Peer ID: %x\n", p.ID)
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
	return nil
}
