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

func handshake(conn net.Conn, t TorrentFile) ([]byte, error) {
	message, err := constructHandshakeMessage(t)
	if err != nil {
		return nil, fmt.Errorf("error constructing peer handshake message: %w", err)
	}
	_, err = conn.Write(message)
	if err != nil {
		return nil, fmt.Errorf("error writing peer handshake message to connection: %w", err)

	}
	respBytes := make([]byte, 68)
	_, err = conn.Read(respBytes)

	if err != nil {
		return nil, fmt.Errorf("error reading peer handshake response: %w", err)
	}
	return respBytes, nil

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
			trackerURL = t.getTrackerURL()
			infoHash   = urlEncodeInfoHash(t.Info.getHexInfoHash())
			peerId     = "leofeopeoluvsanayeli"
			left       = t.Info.length
		)

		r := newTrackerRequest(trackerURL, infoHash, peerId, left)
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

		response, err := handshake(conn, *t)
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
			trackerURL = t.getTrackerURL()
			infoHash   = urlEncodeInfoHash(t.Info.getHexInfoHash())
			peerId     = "leofeopeoluvsanayeli"
			left       = t.Info.length
		)

		treq := newTrackerRequest(trackerURL, infoHash, peerId, left)
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
