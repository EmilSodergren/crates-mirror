extern crate rusqlite;
extern crate time;
extern crate serde;
extern crate serde_json;
extern crate log;
extern crate failure;
#[macro_use] extern crate serde_derive;
#[macro_use] extern crate failure_derive;

use std::io::prelude;
use std::io;
use std::fs::File;
use std::path::PathBuf;
use failure::Error;


const INIT_STMT : &str = "create table crate (
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
);";

const DL : &str = "https://crates.io/api/v1/crates/%s/%s/download";

const INDEX_URL : &str = "https://github.com/rust-lang/crates.io-index";

type MyResult<T> = std::result::Result<T, Error>;
struct Crate {
    name : String,
    vers : String,
    cksum : String,
    size : i64,
    yanked : bool,
}

#[derive(Fail, Debug)]
enum MyErr {
    #[fail(display = "{}", _0)]
    RusqliteErr(#[cause] rusqlite::Error),
    #[fail(display = "{}", _0)]
    Io(#[cause] io::Error),
}

// impl From<io::Error> for MyErr {
    // fn from(err: io::Error) -> Self {
        // MyErr::Io(err)
    // }
// }
// 
// impl From<rusqlite::Error> for MyErr {
    // fn from(err: rusqlite::Error) -> Self {
        // MyErr::RusqliteErr(err)
    // }
// }

#[derive(Deserialize, Debug)]
struct Config {
    crates_path : String,
    registry_path : String,
    db_path : String,
}

fn handle_config(file : String) -> MyResult<Config> {
    let conf = serde_json::from_reader(File::open(file)?)?;
    Ok(conf)
}

fn main() {
    match _main() {
        Err(e) => println!("{:?}", e),
        Ok(_) => println!("done"),
    }
}

fn _main() -> MyResult<()> {
    let f : Vec<String> = std::env::args().collect();
    let file : String = match f.get(1) {
        Some(v) => v.clone(),
        None => String::from("config.json")
    };
    run(handle_config(file)?)?;

    Ok(())
}

fn run(conf : Config) -> MyResult<()> {
    let db = rusqlite::Connection::open(conf.db_path)?;
    Ok(())
}

