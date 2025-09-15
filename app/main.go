package main

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
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

	_, err = f.Read(fileBytes)
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
}

// newTorrentFile serves as a constructor to the TorrentFile struct, given a decoded dictionary of
// a torrent file's contents
func newTorrentFile(dict interface{}) *TorrentFile {
	d := dict.(map[string]interface{})
	infoMap := d["info"].(map[string]interface{})
	info := newInfo(infoMap)
	return &TorrentFile{
		Announce: d["announce"].(string),
		Info:     info,
	}
}

// newTorrentFileFromFilePath serves as a constructor for the TorrentFile struct given a file path
// to a torrent file
func newTorrentFileFromFilePath(filePath string) (*TorrentFile, error) {
	contents, err := parseFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("error parsing torrent file: %w", err)
	}
	decoded, _, err := decode(contents, 0)
	if err != nil {
		return nil, fmt.Errorf("error decoding torrent file path contents: %w", err)
	}

	return newTorrentFile(decoded), nil
}

// newInfo serves as a constructor for the Info struct
func newInfo(infoMap map[string]interface{}) *Info {
	return &Info{
		length:      infoMap["length"].(int),
		name:        infoMap["name"].(string),
		pieceLength: infoMap["piece length"].(int),
		pieces:      infoMap["pieces"].([]byte),
	}
}

// getTrackerUrl returns the URL to the tracker server stored in the Announce key of the torrent
// file.
func (t TorrentFile) getTrackerUrl() string {
	return t.Announce
}

// Returns a string representation of the torrent file
func (t TorrentFile) String() string {
	return fmt.Sprintf(
		"Tracker URL: %s\nLength: %d\nInfo Hash: %x\nPiece Length: %d\nPiece Hashes:\n%s",
		t.Announce, t.Info.length, t.Info.getInfoHash(), t.Info.pieceLength,
		t.Info.getPieceHashesStr(),
	)
}

// getInfoHash returns the SHA1 hash of the bencoded info dictionary
func (i Info) getInfoHash() []byte {
	hasher := sha1.New()
	bencodedBytes := i.bencodeInfo()
	hasher.Write(bencodedBytes)

	sha := hasher.Sum(nil)
	return sha
}

// getHexInfoHash returns the info hash in hexadecimal representation
func (i Info) getHexInfoHash() string {
	return fmt.Sprintf("%x", i.getInfoHash())
}

// bencodeInfo takes all the information in the information dictionary and bencodes it in
// lexicographical order
func (i Info) bencodeInfo() []byte {
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

func (i Info) getPieceHashesStr() string {
	pieceHashes := i.getPieceHashes()
	pieceHashesStr := ""
	for _, h := range pieceHashes {
		pieceHashesStr += fmt.Sprintf("%s\n", h)
	}
	return strings.TrimSpace(pieceHashesStr)
}

// TrackerRequest represents a request made to a tracker server
type TrackerRequest struct {
	TrackerURL string
	InfoHash   string // urlencoded 20-byte long info hash
	PeerId     string
	Port       int
	Uploaded   int
	Downloaded int
	Left       int
	Compact    int
}

// newTrackerRequest serves as a constructor for the TrackerRequest struct.
func newTrackerRequest(
	trackerUrl string, infoHash string, peerId string, left int) *TrackerRequest {

	return &TrackerRequest{
		TrackerURL: trackerUrl,
		InfoHash:   infoHash,
		PeerId:     peerId,
		Port:       6881,
		Uploaded:   0,
		Downloaded: 0,
		Left:       left,
		Compact:    1,
	}
}

// getFullUrl returns the full url sent to a peer for a handshake
func (treq TrackerRequest) getFullUrl() string {
	return fmt.Sprintf(
		"%s?info_hash=%s&peer_id=%s&port=%d&uploaded=%d&downloaded=%d&left=%d&compact=%d",
		treq.TrackerURL, treq.InfoHash, treq.PeerId, treq.Port, treq.Uploaded, treq.Downloaded,
		treq.Left, treq.Compact)
}

func (treq TrackerRequest) SendRequest() ([]byte, error) {
	resp, err := http.Get(treq.getFullUrl())
	if err != nil {
		return nil, fmt.Errorf("error sending request to tracker server: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading tracker response body: %w", err)
	}
	return body, nil
}

// urlEncodeInfoHash URL-encodes a hexadecimal-represented info hash
func urlEncodeInfoHash(infoHash string) string {
	urlEncodedHash := ""
	for i := 0; i < len(infoHash); i += 2 {
		urlEncodedHash += fmt.Sprintf("%%%s%s", string(infoHash[i]), string(infoHash[i+1]))
	}
	return urlEncodedHash
}

type TrackerResponse struct {
	Interval int
	Peers    []string
}

func newTrackerResponse(interval int, peers []string) *TrackerResponse {
	return &TrackerResponse{
		Interval: interval,
		Peers:    peers,
	}
}

func newTrackerResponseFromBytes(response []byte) (*TrackerResponse, error) {
	decoded, _, err := decode(response, 0)
	if err != nil {
		fmt.Println("error decoding tracker response body: ", err)
		return nil, err
	}
	d := decoded.(map[string]interface{})

	var (
		interval  = d["interval"].(int)
		peerBytes = d["peers"].([]byte)
		peers     []string
	)

	for i := 0; i < len(peerBytes); i += 6 {
		port := binary.BigEndian.Uint16(peerBytes[i+4 : i+6])
		address := fmt.Sprintf(
			"%d.%d.%d.%d:%d", peerBytes[i], peerBytes[i+1], peerBytes[i+2], peerBytes[i+3],
			port)
		peers = append(peers, address)
	}

	return newTrackerResponse(interval, peers), nil
}

func (tres TrackerResponse) PeersString() string {
	peers := tres.Peers
	peersString := ""
	for _, peer := range peers {
		peersString += fmt.Sprintf("%s\n", peer)
	}

	return strings.TrimSpace(peersString)
}

func (tres TrackerResponse) getPeers() []string {
	return tres.Peers
}

func constructHandshakeMessage(t TorrentFile) ([]byte, error) {
	var message []byte
	message = append(message, 19)
	message = append(message, []byte("BitTorrent protocol")...)
	message = append(message, make([]byte, 8)...)
	message = append(message, t.Info.getInfoHash()...)
	peerId := make([]byte, 20)
	_, err := rand.Read(peerId)
	if err != nil {
		return nil, fmt.Errorf("error constructing random 20-byte byte slice: %w", err)
	}
	message = append(message, peerId...)
	return message, nil
}

func handshake(conn *net.Conn, t TorrentFile) ([]byte, error) {
	c := *conn
	message, err := constructHandshakeMessage(t)
	if err != nil {
		return nil, fmt.Errorf("error constructing peer handshake message: %w", err)
	}
	_, err = c.Write(message)
	if err != nil {
		return nil, fmt.Errorf("error writing peer handshake message to connection: %w", err)

	}
	respBytes := make([]byte, 68)
	_, err = c.Read(respBytes)

	if err != nil {
		return nil, fmt.Errorf("error reading peer handshake response: %w", err)
	}

	return respBytes, nil

}

func downloadPiece() {

}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	command := os.Args[1]

	switch command {
	case "decode":
		bencodedValue := os.Args[2]

		decoded, _, err := decode([]byte(bencodedValue), 0)
		if err != nil {
			return err
		}
		jsonOutput, err := json.Marshal(decoded)
		fmt.Println(string(jsonOutput))

	case "info":
		filePath := os.Args[2]
		t, err := newTorrentFileFromFilePath(filePath)
		if err != nil {
			return err
		}
		fmt.Println(t.String())
	case "peers":
		filePath := os.Args[2]
		t, err := newTorrentFileFromFilePath(filePath)
		if err != nil {
			return err
		}

		var (
			trackerUrl = t.getTrackerUrl()
			infoHash   = urlEncodeInfoHash(t.Info.getHexInfoHash())
			peerId     = "leofeopeoluvsanayeli"
			left       = t.Info.length
		)

		r := newTrackerRequest(trackerUrl, infoHash, peerId, left)
		body, err := r.SendRequest()
		if err != nil {
			return err
		}

		tres, err := newTrackerResponseFromBytes(body)
		if err != nil {
			return err
		}

		fmt.Println(tres.PeersString())

	case "handshake":
		filePath := os.Args[2]
		peerAddress := os.Args[3]
		t, err := newTorrentFileFromFilePath(filePath)
		if err != nil {
			return err
		}
		conn, err := net.Dial("tcp", peerAddress)
		if err != nil {
			return fmt.Errorf("error opening TCP connection to peer: %w", err)
		}
		defer conn.Close()

		response, err := handshake(&conn, *t)
		if err != nil {
			return err
		}

		peerResponseId := fmt.Sprintf("Peer ID: %x", response[48:])
		fmt.Println(peerResponseId)
	case "download_piece":
		filePath := os.Args[4]

		t, err := newTorrentFileFromFilePath(filePath)
		if err != nil {
			return err
		}

		var (
			trackerUrl = t.getTrackerUrl()
			infoHash   = urlEncodeInfoHash(t.Info.getHexInfoHash())
			peerId     = "leofeopeoluvsanayeli"
			left       = t.Info.length
		)

		treq := newTrackerRequest(trackerUrl, infoHash, peerId, left)
		body, err := treq.SendRequest()
		if err != nil {
			return err
		}
		tres, err := newTrackerResponseFromBytes(body)
		if err != nil {
			return err
		}

		peers := tres.getPeers()
		fmt.Print(peers[0])

	default:
		return fmt.Errorf("unknown command: %s", command)
	}
	return nil
}
