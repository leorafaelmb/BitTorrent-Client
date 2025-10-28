package main

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"

	"github.com/codecrafters-io/bittorrent-starter-go/internal"
	"github.com/codecrafters-io/bittorrent-starter-go/internal/bencode"
	"github.com/codecrafters-io/bittorrent-starter-go/internal/downloader"
	"github.com/codecrafters-io/bittorrent-starter-go/internal/metainfo"
	"github.com/codecrafters-io/bittorrent-starter-go/internal/peer"
	"github.com/codecrafters-io/bittorrent-starter-go/internal/tracker"
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
	decoded, err := bencode.Decode([]byte(bencodedValue))
	if err != nil {
		return err
	}
	jsonOutput, err := json.Marshal(decoded)
	fmt.Println(string(jsonOutput))
	return nil
}

func handleInfo(filePath string) error {
	t, err := metainfo.DeserializeTorrent(filePath)
	if err != nil {
		return err
	}
	fmt.Println(t)
	return nil
}

func handlePeers(filePath string) error {
	t, err := metainfo.DeserializeTorrent(filePath)
	if err != nil {
		return err
	}

	var (
		trackerURL = t.Announce
		infoHash   = metainfo.URLEncodeInfoHash(t.Info.GetHexInfoHash())
		left       = t.Info.Length
	)

	r := tracker.NewTrackerRequest(trackerURL, infoHash, left)
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

	p := peer.Peer{
		AddrPort: &addrPort,
	}

	t, err := metainfo.DeserializeTorrent(filePath)
	if err != nil {
		return err
	}
	err = p.Connect()
	if err != nil {
		return err
	}
	defer p.Conn.Close()

	response, err := p.Handshake(t.Info.InfoHash, false)
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

	t, err := metainfo.DeserializeTorrent(torrentFilePath)
	if err != nil {
		return err
	}

	peers, err := t.GetPeers()
	if err != nil {
		return err
	}

	p := peer.Peer{
		AddrPort: &peers[0],
	}

	if err = p.Connect(); err != nil {
		return err
	}
	defer p.Conn.Close()

	_, err = p.Handshake(t.Info.InfoHash, false)
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
	if msg.ID != internal.MessageUnchoke {
		return fmt.Errorf("incorrect message id: expected 1 got %d", msg.ID)
	}

	pieceLength := uint32(t.Info.PieceLength)
	pieceHash := t.Info.PieceHashes()[pieceIndex]

	if pieceIndex == len(t.Info.Pieces)/20-1 {
		pieceLength = uint32(t.Info.Length) - pieceLength*uint32(len(t.Info.Pieces)/20-1)
	}

	piece, err := p.GetPiece(pieceHash, pieceLength, uint32(pieceIndex))
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

	t, err := metainfo.DeserializeTorrent(torrentFilePath)
	if err != nil {
		return err
	}

	fmt.Println("\nStarting download...")

	peers, err := t.GetPeers()
	if err != nil {
		return err
	}
	fmt.Printf("Found %d peers\n", len(peers))

	// Create Peer objects from addresses
	peerList := make([]peer.Peer, len(peers))
	for i, addr := range peers {
		addrCopy := addr
		peerList[i] = peer.Peer{AddrPort: &addrCopy}
	}

	// Download using multiple concurrent workers with pipelining
	maxWorkers := min(10, len(peerList))
	fmt.Printf("Using %d concurrent workers\n\n", maxWorkers)

	fileBytes, err := downloader.DownloadFile(t, peerList, maxWorkers)
	if err != nil {
		return err
	}

	fmt.Println("\nDownload complete! Saving file(s)...")

	// Use the new SaveFile method which handles both single and multi-file
	if err := t.SaveFile(downloadFilePath, fileBytes); err != nil {
		return fmt.Errorf("error saving file(s): %w", err)
	}

	if t.Info.IsSingleFile() {
		fmt.Printf("File saved to: %s\n", downloadFilePath)
	} else {
		fmt.Printf("Files saved to directory: %s\n", filepath.Join(filepath.Dir(downloadFilePath), t.Info.Name))
	}

	return nil
}

func handleMagnetParse(magnetLink string) error {
	magnet, err := metainfo.DeserializeMagnet(magnetLink)
	if err != nil {
		return err
	}

	fmt.Println("Tracker URL:", magnet.TrackerURL)
	fmt.Println("Info Hash:", magnet.HexInfoHash)
	return nil
}

func handleMagnetHandshake(magnetURL string) error {
	magnet, err := metainfo.DeserializeMagnet(magnetURL)
	treq := tracker.NewTrackerRequest(magnet.TrackerURL, metainfo.URLEncodeInfoHash(magnet.HexInfoHash), 999)
	tres, err := treq.SendRequest()
	if err != nil {
		return err
	}

	p := peer.Peer{AddrPort: &tres.Peers[0]}
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

	t := metainfo.TorrentFile{
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

	t := metainfo.TorrentFile{
		Announce: magnet.TrackerURL,
		Info:     metadata,
	}
	t.Info.InfoHash = magnet.InfoHash

	left := t.Info.Length
	treq := tracker.NewTrackerRequest(magnet.TrackerURL, metainfo.URLEncodeInfoHash(magnet.HexInfoHash), left)
	_, err = treq.SendRequest()
	if err != nil {
		return err
	}

	pieceLength := uint32(t.Info.PieceLength)
	pieceHash := t.Info.PieceHashes()[pieceIndex]
	if pieceIndex == len(t.Info.Pieces)/20-1 {
		pieceLength = uint32(t.Info.Length) - pieceLength*uint32(len(t.Info.Pieces)/20-1)
	}
	// interested msg
	msg, err := p.SendInterested()
	if err != nil {
		return err
	}
	// unchoke
	if msg.ID != internal.MessageUnchoke {
		return fmt.Errorf("incorrect message id: expected 1 got %d", msg.ID)
	}

	piece, err := p.GetPiece(pieceHash, pieceLength, uint32(pieceIndex))
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

	t := metainfo.TorrentFile{
		Announce: magnet.TrackerURL,
		Info:     metadata,
	}
	t.Info.InfoHash = magnet.InfoHash
	peers, err := t.GetPeers()
	if err != nil {
		return err
	}

	peerList := make([]peer.Peer, len(peers))
	for i, addr := range peers {
		peerList[i] = peer.Peer{AddrPort: &addr}
	}

	fileBytes, err := downloader.DownloadFile(&t, peerList, 5)
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

func ConnectToMagnetPeer(magnetURL string) (*peer.Peer, *metainfo.MagnetLink, error) {
	magnet, err := metainfo.DeserializeMagnet(magnetURL)
	if err != nil {
		return nil, nil, err
	}

	treq := tracker.NewTrackerRequest(magnet.TrackerURL,
		metainfo.URLEncodeInfoHash(magnet.HexInfoHash), 999)

	tres, err := treq.SendRequest()
	if err != nil {
		return nil, nil, err
	}

	p := &peer.Peer{AddrPort: &tres.Peers[0]}
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
