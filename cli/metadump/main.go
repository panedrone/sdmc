// metadump is a design-time comparison of the metadata layer with the Java plug-in on a live database.
//
// It prints meta.Metadata in the format of MetaDump.java (sdm-plugins/src_harness), so that the two
// outputs can be compared by plain diff. The password comes from SDM_PWD.
//
//	go run ./cli/metadump -url jdbc:postgresql://host:5432/db -user postgres cli/metadump/my_tests_pg.txt
//	go run ./cli/metadump -url sqlite::memory: internal/meta/testdata/sqlite.txt
//	go run ./cli/metadump -url jdbc:mysql://localhost/go_sdm_probe -user root cli/metadump/mysql.txt
//
// SQLite is also verified in go test (internal/meta/sqlite); this tool is for PostgreSQL.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/panedrone/sdmc/internal/meta"
	"github.com/panedrone/sdmc/internal/meta/metadump"
	"github.com/panedrone/sdmc/internal/meta/mysql"
	"github.com/panedrone/sdmc/internal/meta/pg"
	"github.com/panedrone/sdmc/internal/meta/sqlite"
)

func main() {
	url := flag.String("url", "", "jdbc:postgresql://..., postgres://... or sqlite:<file>")
	user := flag.String("user", "", "user")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: metadump -url <url> [-user <user>] <inputs-file>")
		os.Exit(2)
	}
	if err := run(*url, *user, os.Getenv("SDM_PWD"), flag.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(url, user, pwd, inputs string) error {
	md, err := open(url, user, pwd)
	if err != nil {
		return err
	}
	defer md.Close()
	f, err := os.Open(inputs)
	if err != nil {
		return err
	}
	defer f.Close()
	return metadump.Run(md, f, os.Stdout)
}

func open(url, user, pwd string) (meta.Metadata, error) {
	if file, ok := strings.CutPrefix(url, "sqlite:"); ok {
		return sqlite.Open(file)
	}
	if strings.HasPrefix(url, "jdbc:mysql:") || strings.HasPrefix(url, "mysql:") {
		addr, db, u, p, err := mysql.ConnParams(url, user, pwd)
		if err != nil {
			return nil, err
		}
		return mysql.Open(addr, db, u, p)
	}
	cs, err := pg.ConnString(url, user, pwd)
	if err != nil {
		return nil, err
	}
	return pg.Open(context.Background(), cs)
}
