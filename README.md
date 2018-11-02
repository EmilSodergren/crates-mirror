# Crates Mirror

Download all crates on [Rust official crates site](https://crates.io)
and keep sync with it. This can be used to setup a static mirror site
of https://crates.io.  This can be used with
[cargo-mirror](https://github.com/tennix/cargo-mirror) to make
dependency download faster when building Rust project.

This fork adds more implementations of the crates-mirror
### Requirements
* Python3 (yes python3, python2 is dead)
* Good bandwidth (at least can access aws-s3 service of us region)
* Large hard disk (at least 30G, the current size is 17G, 2018-11-02.)
## Go

### How
1. Clone this repo: `git clone https://github.com/tennix/crates-mirror`
2. go get `github.com/mattn/go-sqlite3`
3. `go install` 
4. Add a config.json file in your current directory, or place it wherever and pass the path as an argument.
    5. Run the program `$GOBIN/crates-mirror [/path/to/config.json]`

The config.json has the folloing structure:
```
   {
       "cratespath":"/path/to/the/downloaded/crate/files",
       "registrypath":"/path/to/the/crates.io-index",
       "dbpath":"/path/to/the/metadata/database"
   }
```
## Python (The original)

### How
1. Clone this repo: `git clone https://github.com/tennix/crates-mirror`
2. Fire a python virtualenv:
   ```
   cd crates-mirror
   pyvenv env
   source env/bin/activate
   pip install -r requirements.txt
   ```
3. Run this program: `python app.py`
4. (Optional)Serve a mirror site:
   ```
   cd crates
   python -m http.server 8000
   ```

*Note*: for production, you should make this program auto-restarted
 when dies ([supervisord](http://supervisord.org) like tools is
 needed). And also use a production web server (nginx, apache etc.) to
 serve the mirror site
