package main

import (
	"fmt"
	"io"
	"net/netip"
	"os"
	"strings"
	"sync"
)

// parseTorrent reads a .torrent file and returns its raw bytes
func parseTorrent(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("error opening torrent file: %w", err)
	}
	defer f.Close()
	fileInfo, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("error reading file info: %w", err)
	}

	fileSize := fileInfo.Size()

	fileBytes := make([]byte, fileSize)

	_, err = io.ReadFull(f, fileBytes)
	if err != nil {
		return nil, fmt.Errorf("error reading file into byte slice: %w", err)
	}

	return fileBytes, nil
}

// TorrentFile represents a parsed .torrent file
type TorrentFile struct {
	Announce string
	Info     *Info
}

// newTorrentFile constructs a TorrentFile given a decoded dictionary of a torrent file's contents
func newTorrentFile(dict interface{}) (*TorrentFile, error) {
	d, ok := dict.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("newTorrent: argument is not a map")
	}
	announce, ok := d["announce"].(string)
	if !ok {
		return nil, fmt.Errorf("newTorrent: announce is not a string")
	}
	infoMap, ok := d["info"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("newTorrent: info value is not a map")
	}
	info, err := newInfo(infoMap)
	if err != nil {
		return nil, fmt.Errorf("error creating Info struct: %w", err)
	}

	info.InfoHash = info.getInfoHash()
	return &TorrentFile{
		Announce: announce,
		Info:     info,
	}, nil
}

// DeserializeTorrent reads and parses a .torrent file from disk.
func DeserializeTorrent(filePath string) (*TorrentFile, error) {
	contents, err := parseTorrent(filePath)
	if err != nil {
		return nil, fmt.Errorf("error parsing torrent file: %w", err)
	}
	decoded, err := Decode(contents)
	if err != nil {
		return nil, fmt.Errorf("error decoding torrent file path contents: %w", err)
	}

	return newTorrentFile(decoded)
}

// String returns a string representation of the torrent file
func (t TorrentFile) String() string {
	filesInfo := ""
	if t.Info.IsSingleFile() {
		filesInfo = fmt.Sprintf("Single File: %s (%d bytes)", t.Info.Name, t.Info.Length)
	} else {
		filesInfo = fmt.Sprintf("Multi-File: %s (root directory)\n", t.Info.Name)
		for i, f := range t.Info.Files {
			path := strings.Join(f.Path, "/")
			filesInfo += fmt.Sprintf("  File %d: %s (%d bytes)\n", i+1, path, f.Length)
		}
	}

	return fmt.Sprintf(
		"Tracker URL: %s\nLength: %d\nInfo Hash: %x\nPiece Length: %d\n%s\nPiece Hashes:\n%s",
		t.Announce, t.Info.Length, t.Info.getInfoHash(), t.Info.PieceLength,
		strings.TrimSpace(filesInfo),
		t.Info.GetPieceHashesStr(),
	)
}

// GetPeers sends a request to the tracker to obtain peers for file download
func (t TorrentFile) GetPeers() ([]netip.AddrPort, error) {
	trackerURL := t.Announce
	infoHash := urlEncodeInfoHash(t.Info.getHexInfoHash())

	treq := newTrackerRequest(trackerURL, infoHash, t.Info.Length)
	tres, err := treq.SendRequest()
	if err != nil {
		return nil, fmt.Errorf("failed to get peers from tracker: %w", err)
	}

	return tres.Peers, nil
}

type PieceWork struct {
	Index  int
	Hash   []byte
	Length uint32
}

type PieceResult struct {
	Index   int
	Payload []byte
}

// DownloadFile orchestrates concurrent download from multiple peers using a worker pool.
func (t TorrentFile) DownloadFile(peers []Peer, maxWorkers int) ([]byte, error) {
	var (
		pieceHashes = t.Info.pieceHashes()
		numPieces   = len(pieceHashes)
		pieceLength = uint32(t.Info.PieceLength)
	)
	workQueue := make(chan *PieceWork, numPieces)
	results := make(chan *PieceResult)

	for i := 0; i < numPieces; i++ {
		length := pieceLength
		// Last piece might be shorter
		if i == numPieces-1 {
			length = uint32(t.Info.Length) - pieceLength*uint32(numPieces-1)
		}
		workQueue <- &PieceWork{
			Index:  i,
			Hash:   pieceHashes[i],
			Length: length,
		}
	}
	close(workQueue)

	var wg sync.WaitGroup
	numWorkers := min(maxWorkers, len(peers))

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(peer Peer) {
			defer wg.Done()
			if err := t.worker(&peer, workQueue, results); err != nil {
				fmt.Printf("Worker error: %v\n", err)
			}
		}(peers[i])
	}

	// Close results channel when workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	pieces := make([][]byte, numPieces)
	for result := range results {
		pieces[result.Index] = result.Payload
	}

	// Assemble file byte slice
	fileBytes := make([]byte, 0, t.Info.Length)
	for _, piece := range pieces {
		fileBytes = append(fileBytes, piece...)
	}

	return fileBytes, nil
}

func (t TorrentFile) worker(peer *Peer, workQueue chan *PieceWork, results chan *PieceResult) error {
	// Connect to peer
	if err := peer.Connect(); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer peer.Conn.Close()

	// Handshake
	if _, err := peer.Handshake(t, false); err != nil {
		return fmt.Errorf("handshake failed: %w", err)
	}

	// Read bitfield
	msg, err := peer.ReadBitfield()
	if err != nil {
		return err
	}

	// Send interested
	msg, err = peer.SendInterested()
	if err != nil {
		return fmt.Errorf("failed to send interested: %w", err)
	}

	// Receive unchoke
	if msg.ID != MessageUnchoke {
		return fmt.Errorf("expected unchoke (1), got %d", msg.ID)
	}

	for work := range workQueue {
		// Check if peer has the piece
		if !peer.Bitfield.HasPiece(work.Index) {
			// Put work back in queue for another peer
			go func(w *PieceWork) {
				workQueue <- w
			}(work)
			continue
		}

		piece, err := peer.getPiece(work.Hash, work.Length, uint32(work.Index))
		if err != nil {
			fmt.Printf("Peer %s failed to download piece %d: %v\n",
				peer.AddrPort.String(), work.Index, err)
			// Put work back in queue to retry
			workQueue <- work
			continue
		}

		results <- &PieceResult{
			Index:   work.Index,
			Payload: piece,
		}

		fmt.Printf("Downloaded piece %d/%d\n", work.Index+1, len(t.Info.Pieces)/20)
	}

	return nil
}
