package main

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"os"
	"path/filepath"
	"log"
	"os/exec"
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

func initialize_db(dbpath string) (*sql.DB, error) {
	var db *sql.DB
	_, dbExistError := os.Stat(dbpath)
	db, err := sql.Open("sqlite3", dbpath)
	if err != nil {
		return nil, err
	}

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
			return err
		}
		fmt.Println(output)
	}
	sql.Exec(

}

func main() {

	var work_dir, err = os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	//var registry_path = filepath.Join(work_dir, "crates.io-index")
	//var crates_path = filepath.Join(work_dir, "crates")
	//var ignore = filepath.Join(registry_path, ".git")
	var db_path = filepath.Join(work_dir, "crates.db")

	_, err = initialize_db(db_path)
	if err != nil {
		log.Fatal(err)
	}
}
