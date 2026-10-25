// Package metadump prints meta.Metadata in the text format of MetaDump.java
// (sdm-plugins/src_harness), to compare the output with the Java plug-in by plain diff.
//
// Input lines: "exec<TAB>sql" (setup, prints nothing), "sql<TAB>jdbc-sql",
// "params<TAB>n<TAB>jdbc-sql", "table<TAB>name", "pk<TAB>name"; '#' is a comment.
package metadump

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/panedrone/sdmc/internal/meta"
)

// Execer is implemented by those that can execute SQL to set up the schema.
type Execer interface {
	Exec(sql string) error
}

func Run(md meta.Metadata, in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p := strings.Split(line, "\t")
		if p[0] == "exec" {
			ex, ok := md.(Execer)
			if !ok {
				return errors.New("'exec' is not supported by this database")
			}
			if err := ex.Exec(p[1]); err != nil {
				return fmt.Errorf("%s: %w", line, err)
			}
			continue
		}
		fmt.Fprintln(out, "== "+line)
		if err := dump(md, p, out); err != nil {
			fmt.Fprintln(out, "ERROR: "+strings.TrimSpace(strings.ReplaceAll(err.Error(), "\n", " ")))
		}
	}
	return sc.Err()
}

func dump(md meta.Metadata, p []string, out io.Writer) error {
	switch p[0] {
	case "sql":
		cols, err := md.SQLColumns(p[1])
		if err != nil {
			return err
		}
		for _, c := range cols {
			fmt.Fprintln(out, col(c))
		}
	case "params":
		n, err := strconv.Atoi(p[1])
		if err != nil {
			return err
		}
		ps, err := md.SQLParams(p[2], n)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "count_unknown=%t count=%d types=[%s]\n", ps.CountUnknown, ps.Count, strings.Join(ps.JavaTypes, ", "))
	case "table":
		t, err := md.Table(p[1])
		if err != nil {
			return err
		}
		for _, c := range t.Columns {
			fmt.Fprintln(out, col(c))
		}
		for _, tc := range t.TableColumns {
			fk := tc.FK
			if fk == "" {
				fk = "null"
			}
			fmt.Fprintf(out, "tc %s data_type=%s size=%s digits=%s ai=%s nullable=%s fk=%s indexed=%t unique=%t\n",
				tc.Name, ptr(tc.DataType), ptr(tc.ColumnSize), ptr(tc.DecimalDigits), ptr(tc.IsAI), ptr(tc.IsNullable),
				fk, tc.IsIndexed, tc.IsUnique)
		}
	case "pk":
		pk, err := md.PKColNames(p[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "pk=[%s]\n", strings.Join(pk, ", "))
	default:
		fmt.Fprintln(out, "?? "+p[0])
	}
	return nil
}

func col(c meta.Column) string {
	return fmt.Sprintf("col %s java=%s ai=%t scale=%d precision=%d display=%d",
		c.Name, c.JavaType, c.IsAI, c.Scale, c.Precision, c.DisplaySize)
}

// ptr prints nil the same way Java prints null.
func ptr[T any](v *T) string {
	if v == nil {
		return "null"
	}
	return fmt.Sprint(*v)
}
