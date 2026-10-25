// dsgen is design-time: it builds the data store for each database from the templates datastore/templates
// into datastore/<db>/<orm>/ (Go), datastore/python/<db>/ (Python) and datastore/cpp/<db>/ (C++). Run it
// from the sdmc root after editing the templates:
//
//	go run ./cli/dsgen
//
// The datastore package test fails if the variants diverge from the templates.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/panedrone/sdmc/datastore"
)

func main() {
	for _, db := range datastore.DBs {
		text, err := datastore.GeneratePython(db)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		dir := filepath.Join("datastore", "python", db)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := os.WriteFile(filepath.Join(dir, datastore.PythonFile), text, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		cpp, err := datastore.GenerateCpp(db)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		dir = filepath.Join("datastore", "cpp", db)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for name, text := range cpp {
			if err := os.WriteFile(filepath.Join(dir, name), text, 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		for _, orm := range datastore.Variants {
			files, err := datastore.Generate(orm, db)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			dir := filepath.Join("datastore", db, orm)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			for name, text := range files {
				if err := os.WriteFile(filepath.Join(dir, name), text, 0o644); err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
			}
		}
	}
}
