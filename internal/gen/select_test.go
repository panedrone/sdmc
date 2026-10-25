package gen

import (
	"reflect"
	"testing"

	"github.com/panedrone/sdmc/internal/sdmxml"
)

func TestSelect(t *testing.T) {
	classes := []sdmxml.Class{{Name: "Task"}, {Name: "TasksDao", Dao: true}, {Name: "sqlx-Group"}}
	for _, c := range []struct {
		args []string
		want map[string]bool
		err  string
	}{
		{args: nil, want: nil},
		{args: []string{",", " "}, want: nil},
		{args: []string{"1,3"}, want: map[string]bool{"Task": true, "sqlx-Group": true}},
		{args: []string{"2", "Task", "2,"}, want: map[string]bool{"Task": true, "TasksDao": true}},
		{args: []string{"sqlx-Group"}, want: map[string]bool{"sqlx-Group": true}},
		{args: []string{"4"}, err: "no class #4: sdm.xml has 3"},
		{args: []string{"0"}, err: "no class #0: sdm.xml has 3"},
		{args: []string{"task"}, err: `no class "task" in sdm.xml`},
	} {
		got, err := Select(classes, c.args, false, false)
		if c.err != "" {
			if err == nil || err.Error() != c.err {
				t.Errorf("%q: err %v, want %q", c.args, err, c.err)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: %v %v, want %v", c.args, got, err, c.want)
		}
	}
}

func TestSelectKind(t *testing.T) {
	classes := []sdmxml.Class{{Name: "Task"}, {Name: "TasksDao", Dao: true}, {Name: "Group"}}
	for _, c := range []struct {
		args         []string
		models, daos bool
		want         map[string]bool
	}{
		{nil, true, false, map[string]bool{"Task": true, "Group": true}},
		{nil, false, true, map[string]bool{"TasksDao": true}},
		{nil, true, true, nil},
		{[]string{"1,2"}, true, false, map[string]bool{"Task": true}},
		{[]string{"Group"}, false, true, map[string]bool{}},
	} {
		got, err := Select(classes, c.args, c.models, c.daos)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q -m=%t -d=%t: %v %v, want %v", c.args, c.models, c.daos, got, err, c.want)
		}
	}
}
