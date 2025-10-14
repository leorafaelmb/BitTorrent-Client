package main

import (
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// parseFile parses a torrent file and returns its bencoded data
func parseFile(path string) ([]byte, error) {
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

type TorrentFile struct {
	Announce string
	Info     *Info
}

// Info represents the info dictionary and its contents in a torrent file
type Info struct {
	length      int
	name        string
	pieceLength int
	pieces      []byte

	files []FileInfo
}

type FileInfo struct {
	Length int
	Path   []string
}

// newTorrentFile serves as a constructor to the TorrentFile struct, given a decoded dictionary of
// a torrent file's contents
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
	return &TorrentFile{
		Announce: announce,
		Info:     info,
	}, nil
}

// DeserializeTorrent serves as a constructor for the TorrentFile struct given a file path
// to a torrent
func DeserializeTorrent(filePath string) (*TorrentFile, error) {
	contents, err := parseFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("error parsing torrent file: %w", err)
	}
	decoded, err := Decode(contents)
	if err != nil {
		return nil, fmt.Errorf("error decoding torrent file path contents: %w", err)
	}

	return newTorrentFile(decoded)
}

// newInfo serves as a constructor for the Info struct
func newInfo(infoMap map[string]interface{}) (*Info, error) {
	length, ok := infoMap["length"].(int)
	if !ok {
		return nil, fmt.Errorf("error accessing info length: not an int")
	}
	name, ok := infoMap["name"].(string)
	if !ok {
		return nil, fmt.Errorf("error accessing info name: not a string")
	}
	pieceLength, ok := infoMap["piece length"].(int)
	if !ok {
		return nil, fmt.Errorf("error accessing info piece length: not an int")
	}
	pieces, ok := infoMap["pieces"].([]byte)
	if !ok {
		return nil, fmt.Errorf("error accessing info pieces: not a byte slice")
	}

	return &Info{
		length:      length,
		name:        name,
		pieceLength: pieceLength,
		pieces:      pieces,
	}, nil
}

// String returns a string representation of the torrent file
func (t TorrentFile) String() string {
	return fmt.Sprintf(
		"Tracker URL: %s\nLength: %d\nInfo Hash: %x\nPiece Length: %d\nPiece Hashes:\n%s",
		t.Announce, t.Info.length, t.Info.getInfoHash(), t.Info.pieceLength,
		t.Info.getPieceHashesStr(),
	)
}

// getInfoHash returns the SHA1 hash of the bencoded info dictionary
func (i Info) getInfoHash() [20]byte {
	infoHash := [20]byte{}
	hasher := sha1.New()
	bencodedBytes := i.serializeInfo()
	hasher.Write(bencodedBytes)

	sha := hasher.Sum(nil)
	copy(infoHash[:], sha)
	return infoHash
}

// getHexInfoHash returns the info hash in hexadecimal representation
func (i Info) getHexInfoHash() string {
	return fmt.Sprintf("%x", i.getInfoHash())
}

// serializeInfo bencodes the Info struct
func (i Info) serializeInfo() []byte {
	lengthB := []byte(fmt.Sprintf("6:lengthi%de", i.length))
	nameB := []byte(fmt.Sprintf("4:name%d:%s", len(i.name), i.name))
	pLB := []byte(fmt.Sprintf("12:piece lengthi%de", i.pieceLength))
	pieces := []byte(fmt.Sprintf("6:pieces%d:", len(i.pieces)))
	pieces = append(pieces, i.pieces...)

	infoB := []byte{'d'}
	infoB = append(append(append(append(append(infoB, lengthB...), nameB...), pLB...), pieces...),
		'e')
	return infoB

}

func (i Info) getPieceHashes() []string {
	var pieceHashes []string
	pieces := i.pieces
	for j := 0; j < len(pieces); j += 20 {
		piece := pieces[j : j+20]
		pieceHashes = append(pieceHashes, fmt.Sprintf("%x", piece))
	}

	return pieceHashes
}

func (i Info) pieceHashes() [][]byte {
	pieces := i.pieces
	var piecesSlice [][]byte
	for j := 0; j < len(pieces); j += 20 {
		piecesSlice = append(piecesSlice, pieces[j:j+20])
	}
	return piecesSlice
}

func (i Info) getPieceHashesStr() string {
	pieceHashes := i.getPieceHashes()
	pieceHashesStr := ""
	for _, h := range pieceHashes {
		pieceHashesStr += fmt.Sprintf("%s\n", h)
	}
	return strings.TrimSpace(pieceHashesStr)
}

// urlEncodeInfoHash URL-encodes a hexadecimal-represented info hash
func urlEncodeInfoHash(infoHash string) string {
	urlEncodedHash := ""
	for i := 0; i < len(infoHash); i += 2 {
		urlEncodedHash += fmt.Sprintf("%%%s%s", string(infoHash[i]), string(infoHash[i+1]))
	}
	return urlEncodedHash
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

func (t TorrentFile) DownloadFile(peers []Peer, maxWorkers int) ([]byte, error) {
	var (
		pieceHashes = t.Info.pieceHashes()
		numPieces   = len(pieceHashes)
		pieceLength = uint32(t.Info.pieceLength)
	)
	workQueue := make(chan *PieceWork, numPieces)
	results := make(chan *PieceResult)

	for i := 0; i < numPieces; i++ {
		length := pieceLength
		// Last piece might be shorter
		if i == numPieces-1 {
			length = uint32(t.Info.length) - pieceLength*uint32(numPieces-1)
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
	fileBytes := make([]byte, 0, t.Info.length)
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
	if _, err := peer.Handshake(t); err != nil {
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
	if msg.ID != 1 {
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

		fmt.Printf("Downloaded piece %d/%d\n", work.Index+1, len(t.Info.pieces)/20)
	}

	return nil
}
