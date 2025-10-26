package main

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"strconv"
)

func runCommand(command string, args []string) error {
	switch command {
	case "decode":
		return handleDecode(args[2])
	case "info":
		return handleInfo(args[2])
	case "peers":
		return handlePeers(args[2])
	case "handshake":
		return handleHandshake(args)
	case "download_piece":
		return handleDownloadPiece(args)
	case "download":
		return handleDownload(args)
	case "magnet_parse":
		return handleMagnetParse(args[2])
	case "magnet_handshake":
		return handleMagnetHandshake(args[2])
	case "magnet_info":
		return handleMagnetInfo(args[2])
	case "magnet_download_piece":
		return handleMagnetDownloadPiece(args)
	case "magnet_download":
		return handleMagnetDownload(args)
	default:

	}
	return nil

}

func handleDecode(bencodedValue string) error {
	decoded, err := Decode([]byte(bencodedValue))
	if err != nil {
		return err
	}
	jsonOutput, err := json.Marshal(decoded)
	fmt.Println(string(jsonOutput))
	return nil
}

func handleInfo(filePath string) error {
	t, err := DeserializeTorrent(filePath)
	if err != nil {
		return err
	}
	fmt.Println(t)
	return nil
}

func handlePeers(filePath string) error {
	t, err := DeserializeTorrent(filePath)
	if err != nil {
		return err
	}

	var (
		trackerURL = t.Announce
		infoHash   = urlEncodeInfoHash(t.Info.getHexInfoHash())
		left       = t.Info.Length
	)

	r := newTrackerRequest(trackerURL, infoHash, left)
	tres, err := r.SendRequest()
	if err != nil {
		return err
	}

	fmt.Println(tres.PeersString())
	return nil
}

func handleHandshake(args []string) error {
	filePath := args[2]
	peerAddress := args[3]

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
	return nil
}

func handleDownloadPiece(args []string) error {
	downloadFilePath := args[3]
	torrentFilePath := args[4]
	pieceIndex, err := strconv.Atoi(os.Args[5])
	if err != nil {
		return err
	}

	t, err := DeserializeTorrent(torrentFilePath)
	if err != nil {
		return err
	}

	peers, err := t.GetPeers()
	if err != nil {
		return err
	}

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
	msg, err := p.ReadBitfield()

	// interested msg
	msg, err = p.SendInterested()
	if err != nil {
		return err
	}
	// unchoke
	if msg.ID != MessageUnchoke {
		return fmt.Errorf("incorrect message id: expected 1 got %d", msg.ID)
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
	return nil
}

func handleDownload(args []string) error {
	downloadFilePath := args[3]
	torrentFilePath := args[4]

	t, err := DeserializeTorrent(torrentFilePath)
	if err != nil {
		return err
	}

	peers, err := t.GetPeers()
	if err != nil {
		return err
	}

	peerList := make([]Peer, len(peers))
	for i, addr := range peers {
		peerList[i] = Peer{AddrPort: &addr}
	}

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
	return nil
}

func handleMagnetParse(magnetLink string) error {
	magnet, err := DeserializeMagnet(magnetLink)
	if err != nil {
		return err
	}

	fmt.Println("Tracker URL:", magnet.TrackerURL)
	fmt.Println("Info Hash:", magnet.HexInfoHash)
	return nil
}

func handleMagnetHandshake(magnetURL string) error {
	magnet, err := DeserializeMagnet(magnetURL)
	treq := newTrackerRequest(magnet.TrackerURL, urlEncodeInfoHash(magnet.HexInfoHash), 999)
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

	eh, err := p.ExtensionHandshake()
	if err != nil {
		return fmt.Errorf("extension handshake failed: %w", err)
	}

	fmt.Printf("Peer ID: %x\n", p.ID)
	fmt.Printf("Peer Metadata Extension ID: %d\n", eh.UtMetadataID)
	return nil
}

func handleMagnetInfo(magnetURL string) error {
	p, magnet, err := ConnectToMagnetPeer(magnetURL)
	defer p.Conn.Close()

	info, err := p.DownloadMetadata(magnet)
	if err != nil {
		return err
	}

	t := TorrentFile{
		Announce: magnet.TrackerURL,
		Info:     info,
	}

	fmt.Println(t)
	return nil
}

func handleMagnetDownloadPiece(args []string) error {
	downloadFilePath := args[3]
	magnetURL := args[4]
	pieceIndex, err := strconv.Atoi(args[5])
	if err != nil {
		return err
	}

	p, magnet, err := ConnectToMagnetPeer(magnetURL)
	defer p.Conn.Close()

	metadata, err := p.DownloadMetadata(magnet)
	if err != nil {
		return err
	}

	t := TorrentFile{
		Announce: magnet.TrackerURL,
		Info:     metadata,
	}
	t.Info.InfoHash = magnet.InfoHash

	left := t.Info.Length
	treq := newTrackerRequest(magnet.TrackerURL, urlEncodeInfoHash(magnet.HexInfoHash), left)
	_, err = treq.SendRequest()
	if err != nil {
		return err
	}

	pieceLength := uint32(t.Info.PieceLength)
	pieceHash := t.Info.pieceHashes()[pieceIndex]
	if pieceIndex == len(t.Info.Pieces)/20-1 {
		pieceLength = uint32(t.Info.Length) - pieceLength*uint32(len(t.Info.Pieces)/20-1)
	}
	// interested msg
	msg, err := p.SendInterested()
	if err != nil {
		return err
	}
	// unchoke
	if msg.ID != MessageUnchoke {
		return fmt.Errorf("incorrect message id: expected 1 got %d", msg.ID)
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
	return nil
}

func handleMagnetDownload(args []string) error {
	downloadFilePath := args[3]
	magnetURl := args[4]

	p, magnet, err := ConnectToMagnetPeer(magnetURl)
	defer p.Conn.Close()

	metadata, err := p.DownloadMetadata(magnet)
	if err != nil {
		return err
	}

	t := TorrentFile{
		Announce: magnet.TrackerURL,
		Info:     metadata,
	}
	t.Info.InfoHash = magnet.InfoHash
	peers, err := t.GetPeers()
	if err != nil {
		return err
	}

	peerList := make([]Peer, len(peers))
	for i, addr := range peers {
		peerList[i] = Peer{AddrPort: &addr}
	}

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
	return nil
}
