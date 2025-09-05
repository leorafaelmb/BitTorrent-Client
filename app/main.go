package main

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	// bencode "github.com/jackpal/bencode-go" // Available if you need it!
)

// Ensures gofmt doesn't remove the "os" encoding/json import (feel free to remove this!)
var _ = json.Marshal

// Example:
// - 5:hello -> hello
// - 10:hello12345 -> hello12345

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
	return fmt.Sprintf("Tracker URL: %s\nLength: %d\nInfo Hash: %s\nPiece Length: %d\nPiece Hashes:\n%s",
		t.Announce, t.Info.length, t.Info.getInfoHash(), t.Info.pieceLength, t.Info.getPieceHashesStr())
}

func (i Info) getInfoHash() string {
	hasher := sha1.New()
	bencodedBytes := i.bencodeInfo()
	hasher.Write(bencodedBytes)

	sha := hasher.Sum(nil)
	return fmt.Sprintf("%x", sha)
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

func main() {
	// You can use print statements as follows for debugging, they'll be visible when running tests.
	fmt.Fprintln(os.Stderr, "Logs from your program will appear here!")

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
		contents, err := parseFile(filePath)
		if err != nil {
			fmt.Println(err)
			return
		}
		decoded, _, err := decode(contents, 0)
		if err != nil {
			fmt.Println(err)
		}
		t := newTorrentFile(decoded)
		fmt.Println(t.String())

	} else if command == "peers" {

	} else {
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
