package main

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"unicode"
	"unicode/utf8"
	// bencode "github.com/jackpal/bencode-go" // Available if you need it!
)

// Ensures gofmt doesn't remove the "os" encoding/json import (feel free to remove this!)
var _ = json.Marshal

// Example:
// - 5:hello -> hello
// - 10:hello12345 -> hello12345
func decode(bencoded []byte, index int) (interface{}, int, error) {
	identifier := rune(bencoded[index])
	if unicode.IsDigit(identifier) {
		decodedString, i, err := decodeString(bencoded, index)
		if utf8.Valid(decodedString) {
			return string(decodedString), i, err
		} else {
			return decodedString, i, err
		}

	} else if identifier == 'i' {
		return decodeInt(bencoded, index)

	} else if identifier == 'l' {
		return decodeList(bencoded, index)

	} else if identifier == 'd' {
		return decodeDict(bencoded, index)

	} else {
		return "", -1, fmt.Errorf("invalid identifier: %s", string(identifier))
	}
}

func decodeString(bencoded []byte, index int) ([]byte, int, error) {
	var firstColonIndex int

	for i := index; i < len(bencoded); i++ {
		if bencoded[i] == ':' {
			firstColonIndex = i
			break
		}
	}
	lengthStr := bencoded[index:firstColonIndex]

	length, err := strconv.Atoi(string(lengthStr))
	if err != nil {
		return make([]byte, 0), -1, fmt.Errorf("error converting lengthStr (%s) to an int: %v", lengthStr, err)
	}
	endIndex := firstColonIndex + 1 + length

	decodedString := bencoded[firstColonIndex+1 : endIndex]

	return decodedString, endIndex, nil
}

func decodeInt(bencoded []byte, index int) (int, int, error) {
	i := index
	for ; bencoded[i] != 'e'; i++ {
	}

	decodedInt, err := strconv.Atoi(string(bencoded[index+1 : i]))
	if err != nil {
		return 0, -1, fmt.Errorf("error converting %s to an int: %v", bencoded[index+1:i], err)
	}

	i++

	return decodedInt, i, nil
}

func decodeList(bencoded []byte, index int) ([]interface{}, int, error) {
	decodedList := make([]interface{}, 0)
	i := index + 1
	for {
		var val interface{}
		var err error

		if bencoded[i] == 'e' {
			i++
			break
		}

		val, i, err = decode(bencoded, i)
		if err != nil {
			return nil, -1, fmt.Errorf("error decoding bencoded value: %v", err)
		}
		decodedList = append(decodedList, val)

	}

	return decodedList, i, nil

}

func decodeDict(bencoded []byte, index int) (map[string]interface{}, int, error) {
	decodedDict := make(map[string]interface{})
	i := index + 1
	for {
		var (
			key []byte
			val interface{}
			err error
		)
		identifier := bencoded[i]

		if identifier == 'e' {
			i++
			break
		}

		key, i, err = decodeString(bencoded, i)
		if err != nil {
			return nil, i, fmt.Errorf("error decoding dict key value: %v", err)
		}

		val, i, err = decode(bencoded, i)
		if err != nil {
			return nil, i, err
		}

		decodedDict[string(key)] = val

	}
	return decodedDict, i, nil
}

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
	return fmt.Sprintf("Tracker URL: %s\nLength: %d\nInfo Hash: %s", t.Announce, t.Info.length, t.Info.getInfoHash())
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
	//fmt.Println(string(append(append(append(infoB, lengthB...), nameB...), pLB...)))
	infoB = append(append(append(append(append(infoB, lengthB...), nameB...), pLB...), pieces...), 'e')
	//	fmt.Println(string(infoB))
	return infoB

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

	} else {
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
