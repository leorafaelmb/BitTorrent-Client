package downloader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/codecrafters-io/bittorrent-starter-go/internal/metainfo"
	"github.com/codecrafters-io/bittorrent-starter-go/internal/peer"
)

type Downloader struct {
	torrent *metainfo.TorrentFile
	peers   []peer.Peer
	config  Config

	workQueue chan *PieceWork
	results   chan *PieceResult
	errors    chan *WorkerError

	ctx        context.Context
	cancelFunc context.CancelFunc
}

func New(t *metainfo.TorrentFile, peers []peer.Peer, opts ...Option) *Downloader {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)

	return &Downloader{
		torrent:    t,
		peers:      peers,
		config:     cfg,
		ctx:        ctx,
		cancelFunc: cancel,
	}

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

// Download orchestrates concurrent download from multiple peers using a worker pool
func (d *Downloader) Download() ([]byte, error) {
	defer d.cancelFunc()

	var (
		pieceHashes = d.torrent.Info.PieceHashes()
		numPieces   = len(pieceHashes)
	)

	d.workQueue = make(chan *PieceWork, numPieces)
	d.results = make(chan *PieceResult, numPieces)
	d.errors = make(chan *WorkerError, len(d.peers))

	if err := d.fillWorkQueue(); err != nil {
		return nil, err
	}

	var wg sync.WaitGroup
	numWorkers := min(d.config.MaxWorkers, len(d.peers))

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(p peer.Peer) {
			defer wg.Done()
			worker := NewWorker(&p, d.torrent, d.config)
			if err := worker.Run(d.ctx, d.workQueue, d.results, d.errors); err != nil {
				d.errors <- &WorkerError{
					PeerAddr: p.AddrPort.String(),
					Phase:    "worker",
					Err:      err,
				}
			}
		}(d.peers[i])
	}

	// Close results when workers are done
	go func() {
		wg.Wait()
		close(d.results)
		close(d.errors)
	}()

	pieces, err := d.collectResults()
	if err != nil {
		return nil, err
	}

	// Assemble file byte slice
	fileBytes := make([]byte, 0, d.torrent.Info.Length)
	for _, piece := range pieces {
		fileBytes = append(fileBytes, piece...)
	}

	return fileBytes, nil
}

func (d *Downloader) fillWorkQueue() error {
	pieceHashes := d.torrent.Info.PieceHashes()
	numPieces := len(pieceHashes)
	pieceLength := uint32(d.torrent.Info.PieceLength)

	for i := 0; i < numPieces; i++ {
		length := pieceLength

		if i == numPieces-1 {
			length = uint32(d.torrent.Info.Length) - pieceLength*uint32(numPieces-1)
		}

		d.workQueue <- &PieceWork{
			Index:  i,
			Hash:   pieceHashes[i],
			Length: length,
		}
	}
	close(d.workQueue)
	return nil
}

// collectResults gathers downloaded pieces
func (d *Downloader) collectResults() ([][]byte, error) {
	numPieces := len(d.torrent.Info.PieceHashes())
	pieces := make([][]byte, numPieces)

	// Progress ticker
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return nil, fmt.Errorf("download timeout")

		case result, ok := <-d.results:
			if !ok {
				// Results channel closed, all workers done
				return pieces, nil
			}

			pieces[result.Index] = result.Payload

		case err := <-d.errors:
			if d.config.Verbose {
				fmt.Printf("Worker error: %v\n", err)
			}

		}
	}
}

// validatePieces checks that all pieces were downloaded
func (d *Downloader) validatePieces(pieces [][]byte) error {
	var missing []int

	for i, piece := range pieces {
		if piece == nil {
			missing = append(missing, i)
		}
	}

	if len(missing) > 0 {
		return &DownloadError{
			TorrentName:  d.torrent.Info.Name,
			FailedPieces: missing,
			TotalPieces:  len(pieces),
		}
	}

	return nil
}

// SaveFile saves downloaded data to appropriate file(s)
func (d *Downloader) SaveFile(downloadPath string, data []byte) error {
	files := d.torrent.Info.GetFiles()

	if d.torrent.Info.IsSingleFile() {
		// Single file: just write it
		return os.WriteFile(downloadPath, data, 0644)
	}
	// Multi-file: create directory structure and split data
	baseDir := filepath.Join(filepath.Dir(downloadPath), d.torrent.Info.Name)
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

func DownloadFile(t *metainfo.TorrentFile, peers []peer.Peer, maxWorkers int, downloadPath string) error {
	d := New(t, peers, WithMaxWorkers(maxWorkers))
	fileBytes, err := d.Download()
	if err != nil {
		return err
	}

	if err = d.SaveFile(downloadPath, fileBytes); err != nil {
		return err
	}
	return nil
}
