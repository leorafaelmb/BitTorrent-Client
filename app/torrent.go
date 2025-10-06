package main

import (
	"crypto/sha1"
	"fmt"
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

// Returns a string representation of the torrent file
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
	bencodedBytes := i.bencodeInfo()
	hasher.Write(bencodedBytes)

	sha := hasher.Sum(nil)
	copy(infoHash[:], sha)
	return infoHash
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

// urlEncodeInfoHash URL-encodes a hexadecimal-represented info hash
func urlEncodeInfoHash(infoHash string) string {
	urlEncodedHash := ""
	for i := 0; i < len(infoHash); i += 2 {
		urlEncodedHash += fmt.Sprintf("%%%s%s", string(infoHash[i]), string(infoHash[i+1]))
	}
	return urlEncodedHash
}
