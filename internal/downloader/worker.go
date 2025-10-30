// internal/downloader/worker.go
package downloader

import (
	"context"
	"fmt"
	"time"

	"github.com/codecrafters-io/bittorrent-starter-go/internal"
	"github.com/codecrafters-io/bittorrent-starter-go/internal/metainfo"
	"github.com/codecrafters-io/bittorrent-starter-go/internal/peer"
)

// Worker handles downloading pieces from a single peer
type Worker struct {
	peer    *peer.Peer
	torrent *metainfo.TorrentFile
	config  Config

	attempted  int
	downloaded int
	failed     int
}

// NewWorker creates a new worker for a peer
func NewWorker(p *peer.Peer, t *metainfo.TorrentFile, cfg Config) *Worker {
	return &Worker{
		peer:    p,
		torrent: t,
		config:  cfg,
	}
}

// Run executes the worker's download loop
func (w *Worker) Run(ctx context.Context, workQueue <-chan *PieceWork, results chan<- *PieceResult, errors chan<- *WorkerError) error {
	// Connect to peer
	if err := w.connect(ctx); err != nil {
		return err
	}
	defer w.peer.Conn.Close()

	// Setup connection
	if err := w.setup(); err != nil {
		return err
	}

	// Download pieces
	return w.downloadLoop(ctx, workQueue, results, errors)
}

// connect establishes connection to the peer
func (w *Worker) connect(ctx context.Context) error {
	// Check context before connecting
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if err := w.peer.Connect(); err != nil {
		return &WorkerError{
			PeerAddr: w.peer.AddrPort.String(),
			Phase:    "connection",
			Err:      err,
		}
	}

	return nil
}

// setup performs handshake and initial protocol exchange
func (w *Worker) setup() error {
	// Handshake
	_, err := w.peer.Handshake(w.torrent.Info.InfoHash, false)
	if err != nil {
		return &WorkerError{
			PeerAddr: w.peer.AddrPort.String(),
			Phase:    "handshake",
			Err:      err,
		}
	}

	// Read bitfield
	_, err = w.peer.ReadBitfield()
	if err != nil {
		return &WorkerError{
			PeerAddr: w.peer.AddrPort.String(),
			Phase:    "bitfield",
			Err:      err,
		}
	}

	// Send interested
	msg, err := w.peer.SendInterested()
	if err != nil {
		return &WorkerError{
			PeerAddr: w.peer.AddrPort.String(),
			Phase:    "interested",
			Err:      err,
		}
	}

	// Wait for unchoke
	if msg.ID != internal.MessageUnchoke {
		return &WorkerError{
			PeerAddr: w.peer.AddrPort.String(),
			Phase:    "unchoke",
			Err:      fmt.Errorf("expected unchoke (1), got %d", msg.ID),
		}
	}

	return nil
}

// downloadLoop processes work items from the queue
func (w *Worker) downloadLoop(ctx context.Context, workQueue <-chan *PieceWork,
	results chan<- *PieceResult, errors chan<- *WorkerError) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case work, ok := <-workQueue:
			if !ok {
				// Queue closed, we're done
				if w.config.Verbose {
					fmt.Printf("Worker %s: attempted=%d, downloaded=%d, failed=%d\n",
						w.peer.AddrPort.String(), w.attempted, w.downloaded, w.failed)
				}
				return nil
			}

			w.attempted++

			// Check if peer has this piece
			if !w.peer.Bitfield.HasPiece(work.Index) {
				continue // Skip pieces this peer doesn't have
			}

			// Download the piece with retries
			piece, err := w.downloadPieceWithRetry(ctx, work)
			if err != nil {
				w.failed++
				errors <- &WorkerError{
					PeerAddr: w.peer.AddrPort.String(),
					Phase:    "download",
					Err:      fmt.Errorf("piece %d: %w", work.Index, err),
				}
				continue
			}

			// Send result
			select {
			case <-ctx.Done():
				return ctx.Err()
			case results <- &PieceResult{
				Index:   work.Index,
				Payload: piece,
			}:
				w.downloaded++
			}
		}
	}
}

// downloadPieceWithRetry attempts to download a piece with retries
func (w *Worker) downloadPieceWithRetry(ctx context.Context, work *PieceWork) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt < w.config.MaxRetries; attempt++ {
		// Check context
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Attempt download
		piece, err := w.peer.GetPiece(work.Hash, work.Length, uint32(work.Index))
		if err == nil {
			return piece, nil // Success!
		}

		lastErr = err

		// Backoff before retry
		if attempt < w.config.MaxRetries-1 {
			backoff := time.Duration(attempt+1) * 100 * time.Millisecond
			if w.config.Verbose {
				fmt.Printf("Worker %s: retry %d/%d for piece %d after %v: %v\n",
					w.peer.AddrPort.String(), attempt+1, w.config.MaxRetries,
					work.Index, backoff, err)
			}

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				// Continue to next retry
			}
		}
	}

	return nil, fmt.Errorf("failed after %d retries: %w", w.config.MaxRetries, lastErr)
}
