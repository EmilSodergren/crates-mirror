package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var dl = "https://crates.io/api/v1/crates/{name}/{version}/download"

const max_connection = 10
const index_url = "https://github.com/rust-lang/crates.io-index"

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
	Yanked bool   `json:"yanked"`
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

func initializeRepo(db *sql.DB, registrypath string) error {
	if _, err := os.Stat(registrypath); os.IsNotExist(err) {
		output, err := exec.Command("git", "clone", index_url).Output()
		if err != nil {
			log.Println(output)
			return err
		}
	}
	err := os.Chdir(registrypath)
	if err != nil {
		return err
	}
	output, err := exec.Command("git", "pull").Output()
	if err != nil {
		log.Println(output)
		return err
	}
	output, err = exec.Command("git", "rev-parse", "HEAD").Output()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(insertUpdateHistoryStmt)
	if err != nil {
		return err
	}
	defer stmt.Close()
	stmt.Exec(string(output), time.Now())
	tx.Commit()

	return nil
}

func loadInfo(db *sql.DB, registrypath, ignore string) error {
	var count int
	err := db.QueryRow("select count(id) from crate").Scan(&count)
	if err != nil {
		return err
	}
	log.Println("Found", count, "rows")
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

func main() {

	log.SetFlags(log.Flags() | log.Llongfile)
	var work_dir, err = os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	var registrypath = filepath.Join(work_dir, "crates.io-index")
	//var crates_path = filepath.Join(work_dir, "crates")
	var ignore = filepath.Join(registrypath, ".git")
	var db_path = filepath.Join(work_dir, "crates.db")

	db, err := initialize_db(db_path)
	if err != nil {
		log.Fatal(err)
	}
	err = initializeRepo(db, registrypath)
	if err != nil {
		log.Fatal(err)
	}
	err = loadInfo(db, registrypath, ignore)
	if err != nil {
		log.Fatal(err)
	}

}
