package metainfo

import (
	"fmt"
	"io"
	"net/netip"
	"os"
	"strings"

	"github.com/codecrafters-io/bittorrent-starter-go/internal/bencode"
	"github.com/codecrafters-io/bittorrent-starter-go/internal/tracker"
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
	info, err := NewInfo(infoMap)
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
	decoded, err := bencode.Decode(contents)
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
	infoHash := URLEncodeInfoHash(t.Info.GetHexInfoHash())

	treq := tracker.NewTrackerRequest(trackerURL, infoHash, t.Info.Length)
	tres, err := treq.SendRequest()
	if err != nil {
		return nil, fmt.Errorf("failed to get peers from tracker: %w", err)
	}

	return tres.Peers, nil
}
