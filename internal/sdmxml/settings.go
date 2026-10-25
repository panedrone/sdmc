package sdmxml

import (
	"fmt"
	"path/filepath"
)

type Settings struct {
	Folders Folders `xml:"folders"`
	Dto     Dto     `xml:"dto"`
	Dao     Dao     `xml:"dao"`
	Jdbc    DB      `xml:"jdbc"`  // IDE plug-ins; sdmc does not read it (cli/genharness does)
	DB      *DB     `xml:"db"`    // the database of sdmc
	GoDB    *DB     `xml:"go-db"` // the former name of <db>: only for RenamedGoDB
	TypeMap TypeMap `xml:"type-map"`
	Macros  []Macro `xml:"macros>macro"`
	// <ide> is an IDE setting, the CLI does not read it
}

type Folders struct {
	Target string `xml:"target,attr"` // for Go: module of go.mod
	SQL    string `xml:"sql,attr"`
}

type Dto struct {
	Scope          string `xml:"scope,attr"`
	Inheritance    string `xml:"inheritance,attr"`
	FieldNamesMode int    `xml:"field-names-mode,attr"` // IDE plug-ins; sdmc ignores it: Go naming only
	Macro          string `xml:"macro,attr"`
}

type Dao struct {
	Scope           string `xml:"scope,attr"`
	FieldAssignMode int    `xml:"field-assign-mode,attr"`
	Macro           string `xml:"macro,attr"`
}

// DB is <db> and <jdbc>; <jdbc> needs no jar and class: the driver is built into the binary.
type DB struct {
	URL  string `xml:"url,attr"`
	User string `xml:"user,attr"`
	Pwd  string `xml:"pwd,attr"`
}

type TypeMap struct {
	Default string `xml:"default,attr"`
	Types   []Type `xml:"type"`
}

type Type struct {
	Detected string `xml:"detected,attr"`
	Target   string `xml:"target,attr"`
}

// Macro is either value (text substitution) or Velocity code in <vm>/<vm-xml>.
type Macro struct {
	Name  string  `xml:"name,attr"`
	Value *string `xml:"value,attr"` // nil: no attribute
	VM    *string `xml:"vm"`
	VMXML *struct {
		Inner string `xml:",innerxml"`
	} `xml:"vm-xml"`
}

// IsVelocity reports a Velocity macro, which the Go version does not have.
func (m *Macro) IsVelocity() bool {
	return m.VM != nil || m.VMXML != nil
}

// RenamedGoDB is the error of a settings.xml with <go-db>, the former name of <db>.
func RenamedGoDB(sdmDir string) error {
	return fmt.Errorf("%s/settings.xml: <go-db> is <db> now, rename the element: <db url=\"...\" user=\"...\" pwd=\"...\"/>", sdmDir)
}

func LoadSettings(sdmDir string) (*Settings, error) {
	var s Settings
	path := filepath.Join(sdmDir, "settings.xml")
	if err := readXML(path, "settings", &s); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, t := range s.TypeMap.Types {
		if seen[t.Detected] {
			return nil, fmt.Errorf("%s: duplicate <type detected=\"%s\"", path, t.Detected)
		}
		seen[t.Detected] = true
	}
	macros := map[string]bool{}
	for _, m := range s.Macros {
		if macros[m.Name] {
			return nil, fmt.Errorf("%s: duplicate <macro name=\"%s\"", path, m.Name)
		}
		macros[m.Name] = true
	}
	return &s, nil
}
