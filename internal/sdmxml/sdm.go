// Package sdmxml reads sdm.xml, external <dao-class> files and settings.xml.
//
// The schemas (sdm.xsd, dao.xsd, settings.xsd) are not checked here: XML is written in an IDE, where
// XSD gives completion and highlighting. Here is what the output would be wrong without:
// attribute defaults from XSD, the order of DAO elements (the order of methods depends on it)
// and unknown DAO elements as an error, not a silent skip.
package sdmxml

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Sdm struct {
	DtoClasses []DtoClass
	DaoClasses []DaoClass
	Classes    []Class // both kinds, in the order of declaration
}

// Class is a <dto-class> or <dao-class> in the common order of sdm.xml.
type Class struct {
	Name string
	Dao  bool
}

func (s *Sdm) UnmarshalXML(d *xml.Decoder, _ xml.StartElement) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "dto-class":
				var c DtoClass
				if err := d.DecodeElement(&c, &t); err != nil {
					return err
				}
				s.DtoClasses = append(s.DtoClasses, c)
				s.Classes = append(s.Classes, Class{Name: c.Name})
			case "dao-class":
				var c DaoClass
				if err := d.DecodeElement(&c, &t); err != nil {
					return err
				}
				s.DaoClasses = append(s.DaoClasses, c)
				s.Classes = append(s.Classes, Class{Name: c.Name, Dao: true})
			default:
				if err := d.Skip(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			return nil
		}
	}
}

type DtoClass struct {
	Name   string  `xml:"name,attr"`
	Ref    string  `xml:"ref,attr"`
	PK     string  `xml:"pk,attr"`   // default "*"
	Auto   string  `xml:"auto,attr"` // default "*"
	Header *string `xml:"header"`    // nil: no element (these are different cases in the template)
	Custom string  `xml:"custom"`
	Fields []Field `xml:"field"`
}

type Field struct {
	Column string `xml:"column,attr"`
	Type   string `xml:"type,attr"`
}

func (c *DtoClass) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	type plain DtoClass
	p := plain{PK: "*", Auto: "*"}
	if err := d.DecodeElement(&p, &start); err != nil {
		return err
	}
	*c = DtoClass(p)
	return nil
}

// DaoClass is a <dao-class> in sdm.xml: either a reference to an external file (Ref) or the elements themselves.
type DaoClass struct {
	Name  string
	Ref   string
	Items []DaoItem // in document order
}

func (c *DaoClass) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var p struct {
		Name  string    `xml:"name,attr"`
		Ref   string    `xml:"ref,attr"`
		Items []DaoItem `xml:",any"`
	}
	if err := d.DecodeElement(&p, &start); err != nil {
		return err
	}
	if err := checkItems(p.Items); err != nil {
		return fmt.Errorf("<dao-class name=\"%s\">: %w", p.Name, err)
	}
	*c = DaoClass{Name: p.Name, Ref: p.Ref, Items: p.Items}
	return nil
}

// DaoItem is one of crud, query, query-list, query-dto, query-dto-list, exec-dml.
type DaoItem struct {
	XMLName        xml.Name
	Method         string      `xml:"method,attr"`
	Ref            string      `xml:"ref,attr"`
	Dto            string      `xml:"dto,attr"`
	ExternalSQL    bool        `xml:"external-sql,attr"`
	ReturnType     string      `xml:"return-type,attr"`
	Table          string      `xml:"table,attr"`           // crud, default "*"
	FetchGenerated bool        `xml:"fetch-generated,attr"` // crud, default true
	Create         *CrudMethod `xml:"create"`
	ReadAll        *CrudMethod `xml:"read-all"`
	Read           *CrudMethod `xml:"read"`
	Update         *CrudMethod `xml:"update"`
	Delete         *CrudMethod `xml:"delete"`
}

// Kind is the element name: "crud", "query", ...
func (it *DaoItem) Kind() string {
	return it.XMLName.Local
}

type CrudMethod struct {
	Method string `xml:"method,attr"`
}

func (it *DaoItem) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	type plain DaoItem
	p := plain{Table: "*", FetchGenerated: true}
	if err := d.DecodeElement(&p, &start); err != nil {
		return err
	}
	*it = DaoItem(p)
	return nil
}

var daoKinds = map[string]bool{
	"crud": true, "query": true, "query-list": true, "query-dto": true, "query-dto-list": true, "exec-dml": true,
}

func checkItems(items []DaoItem) error {
	for _, it := range items {
		if !daoKinds[it.Kind()] {
			return fmt.Errorf("unexpected element <%s>", it.Kind())
		}
	}
	return nil
}

// externalDao is the root of an external DAO file.
type externalDao struct {
	Items []DaoItem `xml:",any"`
}

// LoadSdm reads sdmDir/sdm.xml and pulls in the elements of external <dao-class ref="X.xml">
// (the path is relative to sdmDir, as in the Java plug-in).
func LoadSdm(sdmDir string) (*Sdm, error) {
	var s Sdm
	if err := readXML(filepath.Join(sdmDir, "sdm.xml"), "sdm", &s); err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, c := range s.DtoClasses {
		if err := unique(names, c.Name); err != nil {
			return nil, err
		}
	}
	for i := range s.DaoClasses {
		c := &s.DaoClasses[i]
		if err := unique(names, c.Name); err != nil {
			return nil, err
		}
		if c.Ref == "" {
			continue
		}
		if len(c.Items) > 0 {
			return nil, fmt.Errorf("<dao-class name=\"%s\">: both ref=\"%s\" and inner elements", c.Name, c.Ref)
		}
		var ext externalDao
		path := filepath.Join(sdmDir, c.Ref)
		if err := readXML(path, "dao-class", &ext); err != nil {
			return nil, err
		}
		if err := checkItems(ext.Items); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		c.Items = ext.Items
	}
	return &s, nil
}

func unique(names map[string]bool, name string) error {
	if name == "" {
		return errors.New("empty 'name' in <dto-class> or <dao-class>")
	}
	if names[name] {
		return fmt.Errorf("duplicate name=\"%s\" in sdm.xml", name)
	}
	names[name] = true
	return nil
}

func readXML(path, root string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	data = normalizeAttrs(data)
	var probe struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(data, &probe); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if probe.XMLName.Local != root {
		return fmt.Errorf("%s: <%s> expected, got <%s>", path, root, probe.XMLName.Local)
	}
	if err := xml.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// normalizeAttrs replaces the literal line breaks and tabs of attribute values with spaces, as an XML
// processor must (XML 1.0, 3.3.3) and as JAXB of the plug-in does: a multi-line ref="..." is one line of SQL
// for both. encoding/xml keeps them. A line break written as &#10; stays a line break; text, comments,
// CDATA and processing instructions are left as they are.
func normalizeAttrs(data []byte) []byte {
	res := make([]byte, 0, len(data))
	skipTo := func(i int, end string) int { // copies through the end of a comment, CDATA or PI
		j := bytes.Index(data[i:], []byte(end))
		if j < 0 {
			j = len(data) - i
		} else {
			j += len(end)
		}
		res = append(res, data[i:i+j]...)
		return i + j
	}
	for i := 0; i < len(data); {
		switch {
		case bytes.HasPrefix(data[i:], []byte("<!--")):
			i = skipTo(i, "-->")
		case bytes.HasPrefix(data[i:], []byte("<![CDATA[")):
			i = skipTo(i, "]]>")
		case bytes.HasPrefix(data[i:], []byte("<?")):
			i = skipTo(i, "?>")
		case data[i] == '<': // a tag: attribute values are between quotes
			var quote byte
			for ; i < len(data); i++ {
				c := data[i]
				switch {
				case quote != 0 && c == quote:
					quote = 0
				case quote != 0 && c == '\r':
					if i+1 < len(data) && data[i+1] == '\n' {
						i++ // CR LF is one line break
					}
					c = ' '
				case quote != 0 && (c == '\n' || c == '\t'):
					c = ' '
				case quote == 0 && (c == '"' || c == '\''):
					quote = c
				}
				res = append(res, c)
				if quote == 0 && c == '>' {
					i++
					break
				}
			}
		default:
			res = append(res, data[i])
			i++
		}
	}
	return res
}
