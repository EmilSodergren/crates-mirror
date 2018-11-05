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

const max_connection = 10

const initStmt = `create table crate (
	name text primary key,
	description text,
	documentation text
);
create table crate_version (
    id integer primary key,
    name text,
    version text,
    size integer default 0,
    checksum text,
    yanked integer default 0,
    downloaded integer default 0,
	license text,
    last_update text
);

create table update_history (
    commit_id text,
    timestamp text
);`

type CrateVersion struct {
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
	db.SetMaxOpenConns(1)
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

func initializeRepo(db *sql.DB, registrypath, indexurl string) error {
	if _, err := os.Stat(registrypath); os.IsNotExist(err) {
		os.Chdir(filepath.Dir(registrypath))
		_, err := exec.Command("git", "clone", indexurl, filepath.Base(registrypath)).Output()
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

func loadInfo(db *sql.DB, apiCaller *crateApiCaller, registrypath, ignore string) error {
	var count int
	err := db.QueryRow("select count(id) from crate_version").Scan(&count)
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
			fmt.Println("Reading file", info.Name())
			scanner := bufio.NewScanner(f)
			ci, err := apiCaller.CrateInfo(info.Name())
			if err != nil {
				return err
			}
			_, err = db.Exec("insert into crate (name, description, documentation) values (?,?,?)", ci.Name, ci.Crate.Description, ci.Crate.Documentation)
			if err != nil {
				return err
			}
			for scanner.Scan() {
				var crate CrateVersion
				//req := fmt.Sprintf("%s/%s/%s", dlApi, crate.Name, crate.Vers)
				json.Unmarshal(scanner.Bytes(), &crate)
				_, err := db.Exec("insert into crate_version (name, version, checksum, yanked) values (?, ?, ?, ?)", crate.Name, crate.Vers, crate.Cksum, crate.Yanked)
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

type crateApiCaller struct {
	ServerApi string
}

type Crate struct {
	Description   string `json:"description"`
	Documentation string `json:"documentation"`
}

type CrateInfo struct {
	Name  string
	Crate Crate `json:"crate"`
}

func (c *crateApiCaller) CrateInfo(cratename string) (*CrateInfo, error) {
	req := fmt.Sprintf("%s/%s", c.ServerApi, cratename)
	resp, err := http.Get(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", req, resp.Status)
	}
	var crateInfo = new(CrateInfo)
	var body = new(bytes.Buffer)
	_, err = io.Copy(body, resp.Body)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(body.Bytes(), crateInfo)
	if err != nil {
		return nil, err
	}
	crateInfo.Name = cratename
	return crateInfo, nil
}

func (c *crateApiCaller) Download(cratename, version string) (*bytes.Buffer, error) {
	req := fmt.Sprintf("%s/%s/%s/download", c.ServerApi, cratename, version)
	resp, err := http.Get(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", req, resp.Status)
	}
	var responseData = new(bytes.Buffer)
	_, err = io.Copy(responseData, resp.Body)
	if err != nil {
		return nil, err
	}
	return responseData, nil
}

func downloadCrate(crateChan <-chan CrateVersion, returnCrate chan<- CrateVersion, doneChan chan<- struct{}, cratesdirpath, dlApi string) {
	var caller = crateApiCaller{dlApi}
	for crate := range crateChan {
		filename := fmt.Sprintf("%s-%s.crate", crate.Name, crate.Vers)
		directory := createDirectory(crate.Name, cratesdirpath)
		responseData, err := caller.Download(crate.Name, crate.Vers)
		if err != nil {
			log.Println(err)
			continue
		}
		hash := sha256.New()
		hash.Write(responseData.Bytes())
		if fmt.Sprintf("%x", hash.Sum(nil)) == crate.Cksum {
			cratefilepath := filepath.Join(directory, filename)
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
			log.Printf("Hash mismatch for crate %s-%s. Got %s, Expected %s\n", crate.Name, crate.Vers, fmt.Sprintf("%x", hash.Sum(nil)), crate.Cksum)
			log.Println("The response was", string(responseData.Bytes()))
		}
	}
	doneChan <- struct{}{}
}

type IndexApi struct {
	Dl  string `json:"dl"`
	Api string `json:"api"`
}

func readApi(registrypath string) (*IndexApi, error) {
	apiConfig := filepath.Join(registrypath, "config.json")
	if _, err := os.Stat(apiConfig); os.IsNotExist(err) {
		return nil, err
	}
	var indexApi = new(IndexApi)
	configcontent, err := ioutil.ReadFile(apiConfig)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(configcontent, indexApi)
	if err != nil {
		return nil, err
	}
	return indexApi, nil
}

var updateStmt = "update crate_version set downloaded = ?, size = ?,  last_update = ? where name = ? and version = ?"

func retrieveCrates(db *sql.DB, cratespath, dlApi string) error {
	var crateChan = make(chan CrateVersion)
	var returnCrate = make(chan CrateVersion)
	var doneChan = make(chan struct{})
	workers := 2 * runtime.NumCPU()

	for i := 0; i < workers; i++ {
		go downloadCrate(crateChan, returnCrate, doneChan, cratespath, dlApi)
	}
	go func() {
		for crate := range returnCrate {
			_, err := db.Exec(updateStmt, 1, crate.Size, time.Now(), crate.Name, crate.Vers)
			if err != nil {
				log.Println(err)
			}
		}
		doneChan <- struct{}{}
	}()
	notDownloaded, err := db.Query("select name, version, checksum, yanked from crate_version where downloaded = 0")
	if err != nil {
		return err
	}
	// Read all crates into a slice to only have one database connection active at a time
	var crates []CrateVersion
	for notDownloaded.Next() {
		var crate CrateVersion
		err := notDownloaded.Scan(&crate.Name, &crate.Vers, &crate.Cksum, &crate.Yanked)
		if err != nil {
			return err
		}
		crates = append(crates, crate)
	}
	notDownloaded.Close()
	for _, crate := range crates {
		crateChan <- crate
	}
	// Close and wait for all workers
	close(crateChan)
	for i := 0; i < workers; i++ {
		<-doneChan
	}
	// Close and wait for the data base writer
	close(returnCrate)
	<-doneChan
	return nil
}

type Config struct {
	IndexURL     string `json:"indexurl"`
	CratesPath   string `json:"cratespath"`
	RegistryPath string `json:"registrypath"`
	DbPath       string `json:"dbpath"`
}

func handleArgs() (*Config, error) {
	var configjson string
	if len(os.Args) < 2 {
		fmt.Println("No configfile provided. Using config.json")
		configjson = "config.json"
	} else {
		configjson = os.Args[1]
	}
	var config = new(Config)
	configcontent, err := ioutil.ReadFile(configjson)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(configcontent, &config)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func run(config *Config) error {

	var ignore = filepath.Join(config.RegistryPath, ".git")

	db, err := initialize_db(config.DbPath)
	if err != nil {
		return err
	}
	err = initializeRepo(db, config.RegistryPath, config.IndexURL)
	if err != nil {
		return err
	}
	indexApi, err := readApi(config.RegistryPath)
	if err != nil {
		return err
	}
	var apiCaller = &crateApiCaller{indexApi.Dl}
	err = loadInfo(db, apiCaller, config.RegistryPath, ignore)
	if err != nil {
		return err
	}
	err = retrieveCrates(db, config.CratesPath, indexApi.Dl)
	if err != nil {
		return err
	}
	return nil
}

func main() {

	log.SetFlags(log.Flags() | log.Llongfile)
	config, err := handleArgs()
	if err != nil {
		log.Fatal(err)
	}
	err = run(config)
	if err != nil {
		log.Fatal(err)
	}
}
