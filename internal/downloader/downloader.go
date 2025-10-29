package downloader

import (
	"fmt"
	"github.com/codecrafters-io/bittorrent-starter-go/internal"
	"github.com/codecrafters-io/bittorrent-starter-go/internal/metainfo"
	"github.com/codecrafters-io/bittorrent-starter-go/internal/peer"
	"os"
	"path/filepath"
	"sync"
)

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
func DownloadFile(t *metainfo.TorrentFile, peers []peer.Peer, maxWorkers int) ([]byte, error) {
	var (
		pieceHashes = t.Info.PieceHashes()
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
		go func(peer peer.Peer) {
			defer wg.Done()
			if err := worker(t, &peer, workQueue, results); err != nil {
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

func worker(t *metainfo.TorrentFile, p *peer.Peer, workQueue chan *PieceWork, results chan *PieceResult) error {
	// Connect to p
	if err := p.Connect(); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer p.Conn.Close()

	// Handshake
	if _, err := p.Handshake(t.Info.InfoHash, false); err != nil {
		return fmt.Errorf("handshake failed: %w", err)
	}

	// Read bitfield
	msg, err := p.ReadBitfield()
	if err != nil {
		return err
	}

	// Send interested
	msg, err = p.SendInterested()
	if err != nil {
		return fmt.Errorf("failed to send interested: %w", err)
	}

	// Receive unchoke
	if msg.ID != internal.MessageUnchoke {
		return fmt.Errorf("expected unchoke (1), got %d", msg.ID)
	}

	for work := range workQueue {
		// Check if p has the piece
		if !p.Bitfield.HasPiece(work.Index) {
			// Put work back in queue for another p
			go func(w *PieceWork) {
				workQueue <- w
			}(work)
			continue
		}

		piece, err := p.GetPiece(work.Hash, work.Length, uint32(work.Index))
		if err != nil {
			fmt.Printf("Peer %s failed to download piece %d: %v\n",
				p.AddrPort.String(), work.Index, err)
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

// SaveFile saves downloaded data to appropriate file(s)
func SaveFile(t metainfo.TorrentFile, downloadPath string, data []byte) error {
	files := t.Info.GetFiles()

	if t.Info.IsSingleFile() {
		// Single file: just write it
		return os.WriteFile(downloadPath, data, 0644)
	}
	// Multi-file: create directory structure and split data
	baseDir := filepath.Join(filepath.Dir(downloadPath), t.Info.Name)
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("error creating base directory: %w", err)
	}

	offset := 0
	for _, fileInfo := range files {
		// Construct file path
		pathComponents := append([]string{baseDir}, fileInfo.Path...)
		filePath := filepath.Join(pathComponents...)

		// Create parent directories
		parentDir := filepath.Dir(filePath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("error creating directory %s: %w", parentDir, err)
		}

		// Extract file data
		fileData := data[offset : offset+fileInfo.Length]

		// Write file
		if err := os.WriteFile(filePath, fileData, 0644); err != nil {
			return fmt.Errorf("error writing file %s: %w", filePath, err)
		}

		fmt.Printf("Wrote file: %s (%d bytes)\n", filePath, fileInfo.Length)
		offset += fileInfo.Length
	}

	return nil
}
