package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"unicode"
	// bencode "github.com/jackpal/bencode-go" // Available if you need it!
)

// Ensures gofmt doesn't remove the "os" encoding/json import (feel free to remove this!)
var _ = json.Marshal

// Example:
// - 5:hello -> hello
// - 10:hello12345 -> hello12345
func decode(bencodedString string, index int) (interface{}, int, error) {
	identifier := rune(bencodedString[index])
	if unicode.IsDigit(identifier) {
		return decodeString(bencodedString, index)

	} else if identifier == 'i' {
		return decodeInt(bencodedString, index)

	} else if identifier == 'l' {
		return decodeList(bencodedString, index)

	} else if identifier == 'd' {
		return decodeDict(bencodedString, index)

	} else {
		return "", -1, fmt.Errorf("invalid identifier: %s", string(identifier))
	}
}

func decodeString(bencodedString string, index int) (string, int, error) {
	var firstColonIndex int

	for i := index; i < len(bencodedString); i++ {
		if bencodedString[i] == ':' {
			firstColonIndex = i
			break
		}
	}
	lengthStr := bencodedString[index:firstColonIndex]

	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return "", -1, fmt.Errorf("error converting lengthStr (%s) to an int: %v", lengthStr, err)
	}
	endIndex := firstColonIndex + 1 + length

	decodedString := bencodedString[firstColonIndex+1 : endIndex]

	return decodedString, endIndex, nil
}

func decodeInt(bencodedString string, index int) (int, int, error) {
	i := index
	for ; bencodedString[i] != 'e'; i++ {
	}

	decodedInt, err := strconv.Atoi(bencodedString[index+1 : i])
	if err != nil {
		return 0, -1, fmt.Errorf("error converting %s to an int: %v", bencodedString[index+1:i], err)
	}

	i++

	return decodedInt, i, nil
}

func decodeList(bencodedString string, index int) ([]interface{}, int, error) {
	decodedList := make([]interface{}, 0)
	i := index + 1
	for {
		var val interface{}
		var err error

		if bencodedString[i] == 'e' {
			i++
			break
		}

		val, i, err = decode(bencodedString, i)
		if err != nil {
			return nil, -1, fmt.Errorf("error decoding bencoded value: %v", err)
		}
		decodedList = append(decodedList, val)

	}

	return decodedList, i, nil

}

func decodeDict(bencodedString string, index int) (map[string]interface{}, int, error) {
	decodedDict := make(map[string]interface{})
	i := index + 1
	for {
		var (
			key string
			val interface{}
			err error
		)
		identifier := bencodedString[i]

		if identifier == 'e' {
			i++
			break
		}

		key, i, err = decodeString(bencodedString, i)
		if err != nil {
			return nil, i, fmt.Errorf("error decoding dict key value: %v", err)
		}

		val, i, err = decode(bencodedString, i)
		if err != nil {
			return nil, i, err
		}

		decodedDict[key] = val

	}
	return decodedDict, i, nil
}

func decodeDictFromBytes(fileBytes []byte, index int) (map[string]interface{}, int, error) {
	bencodedString := string(fileBytes)
	decodedDict := make(map[string]interface{})
	i := index + 1
	for {
		var (
			key string
			val interface{}
			err error
		)
		identifier := bencodedString[i]

		if identifier == 'e' {
			i++
			break
		}

		key, i, err = decodeString(bencodedString, i)
		if err != nil {
			return nil, i, fmt.Errorf("error decoding dict key value: %v", err)
		}

		if key == "pieces" {
			lengthStr := string(bencodedString[i+1])
			endIndex, err := strconv.Atoi(lengthStr)
			if err != nil {
				return nil, -1, fmt.Errorf("error turning pieces val length to int")
			}

			piecesBytes := fileBytes[i+2 : endIndex]

			decodedDict[key] = piecesBytes

		} else {
			val, i, err = decode(bencodedString, i)
			if err != nil {
				return nil, i, err
			}

			decodedDict[key] = val
		}

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
	//fmt.Println(fileBytes)
	//fmt.Println(string(fileBytes))

	return fileBytes, nil
}

// parseBytes takes in a byte slice extracted from a torrent file and returns its string representation.
func parseBytes(b []byte) string {
	return ""

}

type TorrentFile struct {
	Announce string
	Info     map[string]interface{}
}

type Info struct {
}

func newTorrentFile(dict interface{}) *TorrentFile {
	d := dict.(map[string]interface{})
	return &TorrentFile{
		Announce: d["announce"].(string),
		Info:     d["info"].(map[string]interface{}),
	}
}

func (t TorrentFile) getTrackerURL() string {
	return t.Announce
}

func (t TorrentFile) String() string {
	return fmt.Sprintf("Tracker URL: %s\nLength: %d\nInfo Hash: %s", t.Announce, t.Info["length"].(int), t.getInfoHash())
}

func (t TorrentFile) getLength() int {
	return t.Info["length"].(int)
}

func (t TorrentFile) getInfoHash() string {
	hasher := sha1.New()
	jsonOutput, _ := json.Marshal(t.Info)

	hasher.Write(jsonOutput)
	sha := hex.EncodeToString(hasher.Sum(nil))
	return sha
}

func main() {
	// You can use print statements as follows for debugging, they'll be visible when running tests.
	fmt.Fprintln(os.Stderr, "Logs from your program will appear here!")

	command := os.Args[1]

	if command == "decode" {
		bencodedValue := os.Args[2]

		decoded, _, err := decode(bencodedValue, 0)
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
		decoded, _, err := decodeDictFromBytes(contents, 0)
		fmt.Println(decoded)
		if err != nil {
			fmt.Println(err)
		}
		//	t := newTorrentFile(decoded)
		//	fmt.Println(t.String())

	} else {
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
