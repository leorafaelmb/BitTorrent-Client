package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
)

// TrackerRequest represents a request made to a tracker server
type TrackerRequest struct {
	TrackerURL string
	InfoHash   string // urlencoded 20-byte info hash
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

func (treq TrackerRequest) SendRequest() (*TrackerResponse, error) {
	resp, err := http.Get(treq.getFullUrl())
	if err != nil {
		return nil, fmt.Errorf("error sending request to tracker server: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading tracker response body: %w", err)
	}

	trackerResponse, err := newTrackerResponseFromBytes(body)
	if err != nil {
		return nil, err
	}
	return trackerResponse, nil
}

type TrackerResponse struct {
	Interval int
	Peers    []netip.AddrPort
}

func newTrackerResponse(interval int, peers []netip.AddrPort) *TrackerResponse {
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
	d, ok := decoded.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("decoded did not return map[string]interface{}")
	}

	var (
		interval  = d["interval"].(int)
		peerBytes = d["peers"].([]byte)
		peers     []netip.AddrPort
	)

	for i := 0; i < len(peerBytes); i += 6 {
		peerAddr, ok := netip.AddrFromSlice(peerBytes[i : i+4])
		if !ok {
			return nil, err
		}
		port := binary.BigEndian.Uint16(peerBytes[i+4 : i+6])

		peerAddrPort := netip.AddrPortFrom(peerAddr, port)
		peers = append(peers, peerAddrPort)
	}

	return newTrackerResponse(interval, peers), err
}

func (tres TrackerResponse) PeersString() string {
	peers := tres.Peers
	peersString := ""
	for _, peer := range peers {
		peersString += fmt.Sprintf("%s\n", peer.String())
	}

	return strings.TrimSpace(peersString)
}
