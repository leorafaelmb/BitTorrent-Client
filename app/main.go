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

func parseFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("error opening torrent file: %v", err)
	}
	defer f.Close()

	fileInfo, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("error reading file info: %v", err)
	}

	fileSize := fileInfo.Size()

	fileBytes := make([]byte, fileSize)

	_, err = f.Read(fileBytes)
	if err != nil {
		return nil, fmt.Errorf("error reading file into byte slice: %v", err)
	}

	//fmt.Println(string(fileBytes))

	return fileBytes, nil
}

type TorrentFile struct {
	Announce string
	Info     *Info
}

type Info struct {
	length      int
	name        string
	pieceLength int
	pieces      []byte
}

func newTorrentFile(dict interface{}) *TorrentFile {
	d := dict.(map[string]interface{})
	infoMap := d["info"].(map[string]interface{})
	info := newInfo(infoMap)
	return &TorrentFile{
		Announce: d["announce"].(string),
		Info:     info,
	}
}

func newTorrentFileFromFilePath(filePath string) *TorrentFile {
	contents, err := parseFile(filePath)
	if err != nil {
		fmt.Println(err)
		return nil
	}
	decoded, _, err := decode(contents, 0)
	if err != nil {
		fmt.Println(err)
	}

	return newTorrentFile(decoded)
}

func newInfo(infoMap map[string]interface{}) *Info {
	return &Info{
		length:      infoMap["length"].(int),
		name:        infoMap["name"].(string),
		pieceLength: infoMap["piece length"].(int),
		pieces:      infoMap["pieces"].([]byte),
	}
}

func (t TorrentFile) getTrackerURL() string {
	return t.Announce
}

func (t TorrentFile) String() string {
	return fmt.Sprintf("Tracker URL: %s\nLength: %d\nInfo Hash: %x\nPiece Length: %d\nPiece Hashes:\n%s",
		t.Announce, t.Info.length, t.Info.getInfoHash(), t.Info.pieceLength, t.Info.getPieceHashesStr())
}

func (i Info) getInfoHash() []byte {
	hasher := sha1.New()
	bencodedBytes := i.bencodeInfo()
	hasher.Write(bencodedBytes)

	sha := hasher.Sum(nil)
	return sha
}

func (i Info) bencodeInfo() []byte {
	lengthB := []byte(fmt.Sprintf("6:lengthi%de", i.length))
	nameB := []byte(fmt.Sprintf("4:name%d:%s", len(i.name), i.name))
	pLB := []byte(fmt.Sprintf("12:piece lengthi%de", i.pieceLength))
	pieces := []byte(fmt.Sprintf("6:pieces%d:", len(i.pieces)))
	pieces = append(pieces, i.pieces...)

	infoB := []byte{'d'}
	infoB = append(append(append(append(append(infoB, lengthB...), nameB...), pLB...), pieces...), 'e')
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

type TrackerRequest struct {
	TrackerURL string
	InfoHash   string //urlencoded 20-byte long info hash
	PeerId     string
	Port       int
	Uploaded   int
	Downloaded int
	Left       int
	Compact    int
}

func newTrackerRequest(trackerUrl string, infoHash string, peerId string, left int) *TrackerRequest {
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

func (tr TrackerRequest) urlEncodeInfoHash() string {
	urlEncodedHash := ""
	ih := tr.InfoHash
	for i := 0; i < len(ih); i += 2 {
		urlEncodedHash += fmt.Sprintf("%%%s%s", string(ih[i]), string(ih[i+1]))
	}
	return urlEncodedHash
}

func (tr TrackerRequest) getFullUrl() string {
	return fmt.Sprintf("%s?info_hash=%s&peer_id=%s&port=%d&uploaded=%d&downloaded=%d&left=%d&compact=%d",
		tr.TrackerURL, tr.urlEncodeInfoHash(), tr.PeerId, tr.Port, tr.Uploaded, tr.Downloaded, tr.Left, tr.Compact)
}

func main() {
	command := os.Args[1]

	if command == "decode" {
		bencodedValue := os.Args[2]

		decoded, _, err := decode([]byte(bencodedValue), 0)
		if err != nil {
			fmt.Println(err)
			return
		}

		jsonOutput, err := json.Marshal(decoded)
		fmt.Println(string(jsonOutput))

	} else if command == "info" {
		filePath := os.Args[2]

		t := newTorrentFileFromFilePath(filePath)
		fmt.Println(t.String())

	} else if command == "peers" {
		filePath := os.Args[2]
		t := newTorrentFileFromFilePath(filePath)

		var (
			trackerUrl = t.Announce
			infoHash   = fmt.Sprintf("%x", t.Info.getInfoHash())
			peerId     = "leofeopeoluvsanayeli"
			left       = t.Info.length
		)

		r := newTrackerRequest(trackerUrl, infoHash, peerId, left)
		fullUrl := r.getFullUrl()

		resp, err := http.Get(fullUrl)
		if err != nil {
			fmt.Println("Error connecting to server: ", err)
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Fatalf("Error reading response body: %v", err)
		}
		decoded, _, err := decode(body, 0)
		if err != nil {
			fmt.Println(err)
			return
		}
		d := decoded.(map[string]interface{})
		p := d["peers"].([]byte)
		for i := 0; i < len(p); i += 6 {
			port := binary.BigEndian.Uint16(p[i+4 : i+6])
			address := fmt.Sprintf("%d.%d.%d.%d:%d", p[i], p[i+1], p[i+2], p[i+3], port)
			fmt.Println(address)
		}

	} else if command == "handshake" {
		filePath := os.Args[2]
		peerAddress := os.Args[3]
		t := newTorrentFileFromFilePath(filePath)
		conn, err := net.Dial("tcp", peerAddress)
		if err != nil {
			fmt.Println(err)
			return
		}
		defer conn.Close()

		var message []byte
		message = append(message, 19)
		message = append(message, []byte("BitTorrent protocol")...)
		message = append(message, make([]byte, 8)...)
		message = append(message, t.Info.getInfoHash()...)
		peerId := make([]byte, 20)
		_, err = rand.Read(peerId)
		if err != nil {
			fmt.Println(err)
			return
		}
		message = append(message, peerId...)
		//fmt.Println(message)
		_, err = conn.Write(message)
		//writer := bufio.NewWriter(conn)
		//n, err := writer.Write(message)
		if err != nil {
			fmt.Println(err)
			return
		}
		//fmt.Println(n)
		//fmt.Println(reader.Buffered())
		respBytes := make([]byte, 68)
		_, err = conn.Read(respBytes)

		//n, err = io.ReadFull(reader, respBytes)
		if err != nil {
			fmt.Println(err)
			return
		}
		peerResponseId := fmt.Sprintf("Peer ID: %x", respBytes[48:])
		fmt.Println(peerResponseId)
	} else {
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
