package main

import (
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
		return "", -1, fmt.Errorf("only strings, integers, and lists are supported at the moment")
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
		return "", -1, err
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
		return 0, -1, err
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
			return nil, -1, err
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
			key interface{}
			val interface{}
			err error
		)
		identifier := bencodedString[i]

		if identifier == 'e' {
			i++
			break
		}

		key, i, err = decode(bencodedString, i)
		if err != nil {
			return nil, i, err
		}

		val, i, err = decode(bencodedString, i)
		if err != nil {
			return nil, i, err
		}

		decodedDict[fmt.Sprintf("%v", key)] = val

	}
	return decodedDict, i, nil
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
	} else {
		fmt.Println("Unknown command: " + command)
		os.Exit(1)
	}
}
