package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/buger/jsonparser"
	shell "github.com/ipfs/go-ipfs-api"
	"github.com/pkg/errors"
	filetype "gopkg.in/h2non/filetype.v1"
)

const maxHeaderSize = 261

var ipfsShell = shell.NewLocalShell()

func logTail(hashChan chan<- string) error {
	resp, err := http.Get("http://localhost:5001/api/v0/log/tail")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(ioutil.Discard, resp.Body)
		return errors.Errorf("unexpected response status code: %d", resp.StatusCode)
	}

	reader := bufio.NewReader(resp.Body)

	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return err
		}

		event, err := jsonparser.GetString(line, "event")
		if err != nil {
			return err
		}

		if event != "handleAddProvider" {
			continue
		}

		key, err := jsonparser.GetString(line, "key")
		if err != nil {
			return err
		}

		hashChan <- key
	}
}

func handleAddProvider(hashChan <-chan string) {
	hashes := map[string]struct{}{}

	for hash := range hashChan {
		if _, ok := hashes[hash]; ok {
			continue
		}

		hashes[hash] = struct{}{}

		func() {
			rc, err := ipfsShell.Cat(hash)
			if err != nil {
				// log.Println(errors.Wrapf(err, "couldn't cat %s", hash))
				return
			}
			defer rc.Close()

			header := make([]byte, maxHeaderSize)

			n, err := rc.Read(header)
			if err != nil {
				// log.Println(errors.Wrapf(err, "couldn't read %s header", hash))
				return
			}
			if n != maxHeaderSize {
				// log.Printf("expected %d bytes, read %d\n", maxHeaderSize, n)
				return
			}

			determinedFileType, err := filetype.Match(header)
			if err != nil {
				io.Copy(ioutil.Discard, rc)
				log.Println(errors.Wrapf(err, "couldn't match %s file type"))
				return
			}

			if determinedFileType == filetype.Unknown {
				io.Copy(ioutil.Discard, rc)
				// log.Println(hash, "unknown file type")
				return
			}

			log.Println(hash, determinedFileType)

			if !filetype.IsImage(header) {
				io.Copy(ioutil.Discard, rc)
				return
			}

			data, err := ioutil.ReadAll(rc)
			if err != nil {
				// log.Println(errors.Wrapf(err, "couldn't read %s data", hash))
				return
			}

			fmt.Printf(
				"\033]1337;File=name=%s;inline=1:%s\a\n",
				hash,
				base64.StdEncoding.EncodeToString(append(header, data...)),
			)
		}()
	}
}

func main() {
	if ipfsShell == nil || !ipfsShell.IsUp() {
		log.Fatalln("IPFS shell isn't running")
	}

	hashChan := make(chan string, 1000)
	defer close(hashChan)

	go handleAddProvider(hashChan)

	log.Fatalln(logTail(hashChan))
}
