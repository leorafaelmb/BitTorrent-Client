package downloader

import (
	"fmt"
	"time"
)

type DownloadError struct {
	TorrentName  string
	FailedPieces []int
	TotalPieces  int
	WorkerErrors map[string]error
}

func (e *DownloadError) Error() string {
	return fmt.Sprintf("download failed for '%s' : %d/%d pieces failed, %d workers had errors",
		e.TorrentName, len(e.FailedPieces), e.TotalPieces, len(e.WorkerErrors))
}

type WorkerError struct {
	PeerAddr string
	Phase    string
	Err      error
}

func (e *WorkerError) Error() string {
	return fmt.Sprintf("worker for peer %s failed during %s: %v", e.PeerAddr, e.Phase, e.Err)
}

type TimeoutError struct {
	Duration         time.Duration
	PiecesTotal      int
	PiecesDownloaded int
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("download timeout after %v: only %d/%d pieces completed",
		e.Duration, e.PiecesDownloaded, e.PiecesTotal)
}
