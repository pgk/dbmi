package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	_ "github.com/lib/pq"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	migrationSeparator string = "/*DOWN*/"
	programName        string = "dbmi"
	version            string = "1.0.0"
	configExample      string = `{
	"db_connection": "postgres://<user>:<pass>@<host>/<yourdbname>?sslmode=disable",
	"db_dbmi_folder": "./migrations",
	"db_dbmi_tablename": "db_migrations"
}
`
)

type Config struct {
	Folder           string `json:"db_dbmi_folder"`
	ConnectionString string `json:"db_connection"`
	Tablename        string `json:"db_dbmi_tablename"`
}

func usage() {
	fmt.Printf("\n%s {COMMAND} [ARGS] [-c]\n", programName)
	fmt.Printf("\nCOMMANDS:\n")
	fmt.Printf("\tinit\t\t\t\tInitialize migrations\n")
	fmt.Printf("\tnew <name>\t\t\tCreate a new migration <name>\n")
	fmt.Printf("\tmigrate <up|down> [amount=all]\tMigrate <direction> by <amount>\n")
	fmt.Printf("\texampleconf\t\t\tEcho the contents of an example config file\n")
	fmt.Printf("\tversion\t\t\t\tDisplay version information\n")
	fmt.Printf("\tusage\t\t\t\tDisplay this message and exit.\n")
	flag.PrintDefaults() // prints default usage
	fmt.Printf("\n")
}

func ver() {
	fmt.Printf("%s v%s\n", programName, version)
}

func exampleConfig() error {
	fmt.Printf(configExample)
	return nil
}

func defaultConfig() *Config {
	config := Config{ConnectionString: "postgres://example:pass@localhost/tablename?ssl-mode=disable", Folder: "./migrations", Tablename: "migrations"}
	return &config
}

func NewConfigFromFile(f string) (*Config, error) {
	jsonFile, err := os.Open(f)
	if err != nil {
		return nil, err
	}

	defer jsonFile.Close()
	byteValue, err := ioutil.ReadAll(jsonFile)
	if err != nil {
		return nil, err
	}

	config := defaultConfig()
	json.Unmarshal([]byte(byteValue), &config)
	return config, nil
}

type Dbmig struct {
	config *Config
	db     *sql.DB
}

func (d *Dbmig) maybeCreateMigrationFolder() error {
	if _, err := os.Stat(d.config.Folder); os.IsNotExist(err) {
		fmt.Println("folder does not exist")
		err := os.Mkdir(d.config.Folder, 0744)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *Dbmig) InitMigrations() error {
	if err := d.maybeCreateMigrationFolder(); err != nil {
		return err
	}

	createMigrationTableStmt := `CREATE TABLE IF NOT EXISTS %s (
		id SERIAL PRIMARY KEY,
		name VARCHAR(256) NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`

	query := fmt.Sprintf(createMigrationTableStmt, d.config.Tablename)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := d.db.ExecContext(ctx, query)

	if err != nil {
		log.Printf("Error %s when creating migrations table", err)
		return err
	}

	rows, err := res.RowsAffected()

	if err != nil {
		log.Printf("Error %s when getting rows affected", err)
		return err
	}

	log.Printf("Rows affected: %d", rows)
	return err
}

func (d *Dbmig) Migrate(args []string) error {
	fmt.Printf("%v\n", args)
	if len(args) == 0 || args[0] != "migrate" {
		return fmt.Errorf("Invalid call %v", args)
	}

	migrateDown := false
	amount := 1

	if len(args) > 1 && args[1] == "down" {
		migrateDown = true
	}

	if len(args) > 2 {
		if i, err := strconv.Atoi(args[2]); err == nil {
			amount = i
		}
	}

	migrationFiles := migrationFilenames(d.config.Folder)
	log.Printf("filenames of migrations: %v", migrationFiles)

	var applied []string
	if migrateDown {
		applied = appliedMigrations(d, amount, true)
		log.Printf("Applied migrations: %v", applied)
		for _, p := range applied {
			if err := applyMigration(d, p, "down"); err != nil {
				return err
			}
		}
	} else {
		applied = appliedMigrations(d, -1, false)
		log.Printf("Applied migrations: %v", applied)
		pending := diffOf(migrationFiles, applied)

		for i, p := range pending {
			if i >= amount {
				return nil
			}

			if err := applyMigration(d, p, "up"); err != nil {
				return err
			}
		}
	}

	return nil
}

func applyMigration(d *Dbmig, fname string, direction string) error {
	fpath := fmt.Sprintf("%s/%s", d.config.Folder, fname)
	data, err := ioutil.ReadFile(fpath)
	if err != nil {
		return err
	}

	migrationData := string(data)
	spl := strings.Split(migrationData, migrationSeparator)
	if len(spl) != 2 {
		return nil
	}

	up := spl[0]
	down := spl[1]
	var stmt string

	if direction == "down" {
		stmt = down
	} else {
		stmt = up
	}
	log.Printf("Applying: %s\n %s\n", fpath, stmt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db := d.db

	_, err = db.ExecContext(ctx, stmt)

	if err != nil {
		log.Printf("Error Applying migration: %v\n", err)
		return err
	}

	var doneStmt string

	if direction == "down" {
		doneStmt = fmt.Sprintf(`DELETE FROM %s WHERE name = $1 RETURNING *`, d.config.Tablename)
	} else {
		doneStmt = fmt.Sprintf(`INSERT INTO %s (name) VALUES ($1) RETURNING *`, d.config.Tablename)
	}

	log.Printf("Done action: %s\n", doneStmt)

	_, err = db.ExecContext(ctx, doneStmt, fname)

	if err != nil {
		log.Printf("Error Applying migration doneAction: %v\n", err)
		return err
	}

	return nil
}

func toSet(a []string) map[string]bool {
	amap := map[string]bool{}
	for _, s := range a {
		amap[s] = true
	}

	return amap
}

func diffOf(a, b []string) []string {
	result := make([]string, 0)
	amap := toSet(a)
	bmap := toSet(b)

	for key, _ := range amap {
		_, isset := bmap[key]
		if !isset {
			result = append(result, key)
		}
	}

	return result
}

func appliedMigrations(d *Dbmig, amount int, reverse bool) []string {
	names := make([]string, 0)

	var query string
	if reverse && amount > 0 {
		query = fmt.Sprintf("SELECT name from %s ORDER BY created_at DESC, id LIMIT %d", d.config.Tablename, amount)
	} else {
		query = fmt.Sprintf("SELECT name from %s ORDER BY created_at, id", d.config.Tablename)
	}

	rows, err := d.db.Query(query)

	if err != nil {
		log.Printf("DB Error: %s\n", err)
		return names
	}

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			log.Fatal(err)
		}
		names = append(names, name)
	}

	// Check for errors from iterating over rows.
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}

	// log.Printf("migrations: %v", strings.Join(names, ", "))
	return names

}

func migrationFilenames(dir string) []string {
	fnames := make([]string, 0)
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("prevent panic by handling failure accessing a path %q: %v\n", p, err)
			return err
		}

		if path.Ext(p) == ".sql" {
			file := path.Base(p)
			fnames = append(fnames, file)
		}

		return nil
	})

	if err != nil {
		fmt.Printf("error walking the path %q: %v\n", dir, err)
		return fnames
	}

	return fnames
}
func (d *Dbmig) NewMigration(args []string) error {
	if len(args) < 2 || args[0] != "new" {
		return fmt.Errorf("Invalid number of args %v", args)
	}

	now := time.Now()
	re := regexp.MustCompile(`[\W\r?\n]+`)
	name := re.ReplaceAllString(args[1], "_")
	fullName := fmt.Sprintf("%d_%s.sql", now.Unix(), name)

	fmt.Println(fullName)
	sqlTemplate := `-- put your up-migration here.

%s
-- put your down-migration here.

`

	sql := fmt.Sprintf(sqlTemplate, migrationSeparator)

	fmt.Println(sql)
	migrationFolder := d.config.Folder

	fullPath := fmt.Sprintf("%s/%s", migrationFolder, fullName)
	f, err := os.Create(fullPath)

	if err != nil {
		return err
	}

	defer f.Close()

	l, err := f.WriteString(sql)

	if err != nil {
		return err
	}

	fmt.Printf("Schema change created: %s (%d bytes written)\n", fullPath, l)

	return nil
}

func main() {
	config := defaultConfig()
	var configFile string
	var help bool

	flag.StringVar(&configFile, "c", "dbm.conf.json", "Change default config file")
	flag.BoolVar(&help, "h", false, "Get help")
	flag.Usage = usage
	flag.Parse()

	config, err := NewConfigFromFile(configFile)

	if err != nil {
		log.Fatal(err)
	}

	args := flag.Args()

	if len(args) == 0 {
		usage()
		return
	}

	db, err := sql.Open("postgres", config.ConnectionString)

	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	dbmig := &Dbmig{config, db}

	command := args[0]

	switch command {
	case "version":
		ver()
		break
	case "exampleconf":
		exampleConfig()
		break
	case "init":
		if err := dbmig.InitMigrations(); err != nil {
			log.Fatal(fmt.Sprintf("%s", err))
		}
		break
	case "new":
		if err := dbmig.NewMigration(args); err != nil {
			log.Fatal(fmt.Sprintf("%s", err))
		}
		break
	case "migrate":
		if err := dbmig.Migrate(args); err != nil {
			log.Fatal(fmt.Sprintf("%s", err))
		}
		break
	default:
		usage()
		break
	}
}
