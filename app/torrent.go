package main

import (
	"crypto/sha1"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strings"
	"sync"
)

// parseTorrent parses a torrent file and returns its bencoded data
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

type TorrentFile struct {
	Announce string
	Info     *Info
}

// Info represents the info dictionary and its contents in a torrent file
type Info struct {
	Length      int
	Name        string
	PieceLength int
	Pieces      []byte
	InfoHash    [20]byte
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

	info.InfoHash = info.getInfoHash()
	return &TorrentFile{
		Announce: announce,
		Info:     info,
	}, nil
}

// DeserializeTorrent serves as a constructor for the TorrentFile struct given a file path
// to a torrent
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
		Length:      length,
		Name:        name,
		PieceLength: pieceLength,
		Pieces:      pieces,
	}, nil
}

// String returns a string representation of the torrent file
func (t TorrentFile) String() string {
	return fmt.Sprintf(
		"Tracker URL: %s\nLength: %d\nInfo Hash: %x\nPiece Length: %d\nPiece Hashes:\n%s",
		t.Announce, t.Info.Length, t.Info.getInfoHash(), t.Info.PieceLength,
		t.Info.GetPieceHashesStr(),
	)
}

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
	lengthB := []byte(fmt.Sprintf("6:lengthi%de", i.Length))
	nameB := []byte(fmt.Sprintf("4:name%d:%s", len(i.Name), i.Name))
	pLB := []byte(fmt.Sprintf("12:piece lengthi%de", i.PieceLength))
	pieces := []byte(fmt.Sprintf("6:pieces%d:", len(i.Pieces)))
	pieces = append(pieces, i.Pieces...)

	infoB := []byte{'d'}
	infoB = append(append(append(append(append(infoB, lengthB...), nameB...), pLB...), pieces...),
		'e')
	return infoB

}

func (i Info) GetPieceHashes() []string {
	var pieceHashes []string
	pieces := i.Pieces
	for j := 0; j < len(pieces); j += 20 {
		piece := pieces[j : j+20]
		pieceHashes = append(pieceHashes, fmt.Sprintf("%x", piece))
	}

	return pieceHashes
}

func (i Info) pieceHashes() [][]byte {
	pieces := i.Pieces
	var piecesSlice [][]byte
	for j := 0; j < len(pieces); j += 20 {
		piecesSlice = append(piecesSlice, pieces[j:j+20])
	}
	return piecesSlice
}

func (i Info) GetPieceHashesStr() string {
	pieceHashes := i.GetPieceHashes()
	pieceHashesStr := ""
	for _, h := range pieceHashes {
		pieceHashesStr += fmt.Sprintf("%s\n", h)
	}
	return strings.TrimSpace(pieceHashesStr)
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

		fmt.Printf("Downloaded piece %d/%d\n", work.Index+1, len(t.Info.Pieces)/20)
	}

	return nil
}
