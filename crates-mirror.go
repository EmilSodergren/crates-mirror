package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// variables are "name" and "version"
var dl = "https://crates.io/api/v1/crates/%s/%s/download"

const index_url = "https://github.com/rust-lang/crates.io-index"

const max_connection = 10

const initStmt = `create table crate (
    id integer primary key,
    name text,
    version text,
    size integer default 0,
    checksum text,
    yanked integer default 0,
    downloaded integer default 0,
    last_update text
);
create table update_history (
    commit_id text,
    timestamp text
);`

type Crate struct {
	Name   string `json:"name"`
	Vers   string `json:"vers"`
	Cksum  string `json:"cksum"`
	Size   int64
	Yanked bool `json:"yanked"`
}

func initialize_db(dbpath string) (*sql.DB, error) {
	var db *sql.DB
	_, dbExistError := os.Stat(dbpath)
	db, err := sql.Open("sqlite3", dbpath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	db.Exec("PRAGMA journal_mode=WAL")

	if os.IsNotExist(dbExistError) {
		_, err = db.Exec(initStmt)
		if err != nil {
			return nil, err
		}
	}

	return db, nil
}

const insertUpdateHistoryStmt = "insert into update_history values(?, ?)"

func initializeRepo(db *sql.DB, registrypath string) error {
	if _, err := os.Stat(registrypath); os.IsNotExist(err) {
		os.Chdir(filepath.Dir(registrypath))
		_, err := exec.Command("git", "clone", index_url, filepath.Base(registrypath)).Output()
		if err != nil {
			return err
		}
	}
	err := os.Chdir(registrypath)
	if err != nil {
		return err
	}
	output, err := exec.Command("git", "pull").Output()
	if err != nil {
		return err
	}
	output, err = exec.Command("git", "rev-parse", "HEAD").Output()
	_, err = db.Exec(insertUpdateHistoryStmt, string(output), time.Now())
	return err
}

func loadInfo(db *sql.DB, registrypath, ignore string) error {
	var count int
	err := db.QueryRow("select count(id) from crate").Scan(&count)
	if err != nil {
		return err
	}
	if count != 0 { // info already loaded
		return nil
	}
	return filepath.Walk(registrypath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == ignore {
			return filepath.SkipDir
		}
		if info.Name() == "config.json" {
			return nil
		}
		if !info.IsDir() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				var crate Crate
				json.Unmarshal(scanner.Bytes(), &crate)
				_, err := db.Exec("insert into crate (name, version, checksum, yanked) values (?, ?, ?, ?)", crate.Name, crate.Vers, crate.Cksum, crate.Yanked)
				if err != nil {
					return err
				}
			}
			if err := scanner.Err(); err != nil {
				return err
			}
		}
		return nil
	})
}

func createDirectory(name, cratespath string) string {
	var directory string
	if len(name) == 1 {
		directory = filepath.Join(cratespath, "1", name)
	} else if len(name) == 2 {
		directory = filepath.Join(cratespath, "2", name)
	} else if len(name) == 3 {
		directory = filepath.Join(cratespath, "3", name[:1], name)
	} else {
		directory = filepath.Join(cratespath, name[:2], name[2:4], name)
	}
	os.MkdirAll(directory, 0755)
	return directory
}

func downloadCrate(crateChan <-chan Crate, returnCrate chan<- Crate, doneChan chan<- struct{}, cratesdirpath string) {
	for crate := range crateChan {
		filename := fmt.Sprintf("%s-%s.crate", crate.Name, crate.Vers)
		directory := createDirectory(crate.Name, cratesdirpath)
		req := fmt.Sprintf(dl, crate.Name, crate.Vers)
		resp, err := http.Get(req)
		if err != nil {
			log.Println(err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			log.Println(req, ": ", resp.Status)
			continue
		}
		cratefilepath := filepath.Join(directory, filename)
		if err != nil {
			log.Println(err)
			continue
		}
		var responseData = new(bytes.Buffer)
		io.Copy(responseData, resp.Body)
		resp.Body.Close()
		hash := sha256.New()
		hash.Write(responseData.Bytes())
		if fmt.Sprintf("%x", hash.Sum(nil)) == crate.Cksum {
			out, err := os.Create(cratefilepath)
			if err != nil {
				log.Println(err)
			}
			fmt.Println("Downloaded", filename)
			crate.Size, err = io.Copy(out, responseData)
			if err != nil {
				log.Println(err)
			}
			out.Close()
			returnCrate <- crate
		} else {
			log.Printf("Hash mismatch for file %s. Got %s, Expected %s\n", crate.Name, fmt.Sprintf("%x", hash.Sum(nil)), crate.Cksum)
		}
	}
	doneChan <- struct{}{}

}

var updateStmt = "update crate set downloaded = ?, size = ?,  last_update = ? where name = ? and version = ?"

func retrieveCrates(db *sql.DB, cratespath string) error {
	var crateChan = make(chan Crate)
	var returnCrate = make(chan Crate)
	var doneChan = make(chan struct{})
	workers := 2 * runtime.NumCPU()

	for i := 0; i < workers; i++ {
		go downloadCrate(crateChan, returnCrate, doneChan, cratespath)
	}
	go func() {
		for crate := range returnCrate {
			_, err := db.Exec(updateStmt, 1, crate.Size, time.Now(), crate.Name, crate.Vers)
			if err != nil {
				log.Println(err)
			}
		}
	}()
	notDownloaded, err := db.Query("select name, version, checksum, yanked from crate where downloaded = 0")
	if err != nil {
		return err
	}
	var crates []Crate
	for notDownloaded.Next() {
		var crate Crate
		err := notDownloaded.Scan(&crate.Name, &crate.Vers, &crate.Cksum, &crate.Yanked)
		if err != nil {
			return err
		}
		crates = append(crates, crate)
	}
	for _, crate := range crates {
		crateChan <- crate
	}
	for i := 0; i < workers; i++ {
		<-doneChan
	}
	close(returnCrate)
	return nil
}

type Config struct {
	CratesPath   string `json:"cratespath"`
	RegistryPath string `json:"registrypath"`
	DbPath       string `json:"dbpath"`
}

func main() {

	log.SetFlags(log.Flags() | log.Llongfile)
	var configjson string
	if len(os.Args) < 2 {
		fmt.Println("No configfile provided. Using config.json")
		configjson = "config.json"
	} else {
		configjson = os.Args[1]
	}
	var config Config
	configcontent, err := ioutil.ReadFile(configjson)
	if err != nil {
		log.Fatal(err)
	}
	json.Unmarshal(configcontent, &config)
	var ignore = filepath.Join(config.RegistryPath, ".git")

	db, err := initialize_db(config.DbPath)
	if err != nil {
		log.Fatal(err)
	}
	err = initializeRepo(db, config.RegistryPath)
	if err != nil {
		log.Fatal(err)
	}
	err = loadInfo(db, config.RegistryPath, ignore)
	if err != nil {
		log.Fatal(err)
	}
	err = retrieveCrates(db, config.CratesPath)
	if err != nil {
		log.Fatal(err)
	}
}
