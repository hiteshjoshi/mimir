package dbgen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"strings"
	"text/template"
)

//DBTEMPLATE the main mart of the db source
const DBTEMPLATE = `//file genereated with github.com/microo8/dbgen DO NOT MODIFY!
package main
{{$gen := .}}
import (
    "bytes"
	"fmt"
	"math"
    "math/rand"

    "github.com/syndtr/goleveldb/leveldb"
    "github.com/syndtr/goleveldb/leveldb/util"
    "github.com/syndtr/goleveldb/leveldb/iterator"
)

//Constants for int encoding
const (
	IntMin      = 0x80
	intMaxWidth = 8
	intZero     = IntMin + intMaxWidth
	intSmall    = IntMax - intZero - intMaxWidth
	IntMax		= 0xfd
)

//lexDump functions for encoding values to lexicographically ordered byte array
func lexDumpString(v string) []byte {
	return []byte(v)
}

func lexDumpBool(v bool) []byte {
	if v {
		return []byte{1}
	}
	return []byte{0}
}

func lexDumpInt(v int) []byte {
	return lexDumpInt64(int64(v))
}

func lexDumpInt64(v int64) []byte {
	if v < 0 {
		switch {
		case v >= -0xff:
			return []byte{IntMin+7, byte(v)}
		case v >= -0xffff:
			return []byte{IntMin+6, byte(v>>8), byte(v)}
		case v >= -0xffffff:
			return []byte{IntMin+5, byte(v>>16), byte(v>>8), byte(v)}
		case v >= -0xffffffff:
			return []byte{IntMin+4, byte(v>>24), byte(v>>16), byte(v>>8), byte(v)}
		case v >= -0xffffffffff:
			return []byte{IntMin+3, byte(v>>32), byte(v>>24), byte(v>>16), byte(v>>8), byte(v)}
		case v >= -0xffffffffffff:
			return []byte{IntMin+2, byte(v>>40), byte(v>>32), byte(v>>24), byte(v>>16),	byte(v>>8), byte(v)}
		case v >= -0xffffffffffffff:
			return []byte{IntMin+1, byte(v>>48), byte(v>>40), byte(v>>32), byte(v>>24), byte(v>>16), byte(v>>8), byte(v)}
		default:
			return []byte{IntMin, byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32), byte(v>>24), byte(v>>16), byte(v>>8), byte(v)}
		}
	}
	return lexDumpUint64(uint64(v))
}

func lexDumpUint64(v uint64) []byte {
	switch {
	case v <= intSmall:
		return []byte{intZero+byte(v)}
	case v <= 0xff:
		return []byte{IntMax-7, byte(v)}
	case v <= 0xffff:
		return []byte{IntMax-6, byte(v>>8), byte(v)}
	case v <= 0xffffff:
		return []byte{IntMax-5, byte(v>>16), byte(v>>8), byte(v)}
	case v <= 0xffffffff:
		return []byte{IntMax-4, byte(v>>24), byte(v>>16), byte(v>>8), byte(v)}
	case v <= 0xffffffffff:
		return []byte{IntMax-3, byte(v>>32), byte(v>>24), byte(v>>16), byte(v>>8), byte(v)}
	case v <= 0xffffffffffff:
		return []byte{IntMax-2, byte(v>>40), byte(v>>32), byte(v>>24), byte(v>>16), byte(v>>8), byte(v)}
	case v <= 0xffffffffffffff:
		return []byte{IntMax-1, byte(v>>48), byte(v>>40), byte(v>>32), byte(v>>24), byte(v>>16), byte(v>>8), byte(v)}
	default:
		return []byte{IntMax, byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32), byte(v>>24), byte(v>>16), byte(v>>8), byte(v)}
	}
}

func lexLoadInt(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, fmt.Errorf("insufficient bytes to decode uvarint value")
	}
	length := int(b[0]) - intZero
	if length < 0 {
		length = -length
		remB := b[1:]
		if len(remB) < length {
			return 0, fmt.Errorf("insufficient bytes to decode uvarint value: %s", remB)
		}
		var v int
		// Use the ones-complement of each encoded byte in order to build
		// up a positive number, then take the ones-complement again to
		// arrive at our negative value.
		for _, t := range remB[:length] {
			v = (v << 8) | int(^t)
		}
		return ^v, nil
	}

	v, err := lexLoadUint(b)
	if err != nil {
		return 0, err
	}
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("varint %d overflows int64", v)
	}
	return int(v), nil
}

func lexLoadUint(b []byte) (uint, error) {
	if len(b) == 0 {
		return 0, fmt.Errorf("insufficient bytes to decode uvarint value")
	}
	length := int(b[0]) - intZero
	b = b[1:] // skip length byte
	if length <= intSmall {
		return uint(length), nil
	}
	length -= intSmall
	if length < 0 || length > 8 {
		return 0, fmt.Errorf("invalid uvarint length of %d", length)
	} else if len(b) < length {
		return 0, fmt.Errorf("insufficient bytes to decode uvarint value: %v", b)
	}
	var v uint
	// It is faster to range over the elements in a slice than to index
	// into the slice on each loop iteration.
	for _, t := range b[:length] {
		v = (v << 8) | uint(t)
	}
	return v, nil
}

//Encode is an function for encoding objects to bytes
type Encode func(interface{}) ([]byte, error)
//Decode is an function for decoding objects from bytes
type Decode func([]byte, interface{}) error

//DB handler to the db
type DB struct {
    db     *leveldb.DB
	encode Encode
	decode Decode
}

//OpenDB opens the database
func OpenDB(path string, encode Encode, decode Decode) (*DB, error) {
    db, err := leveldb.OpenFile(path, nil)
    if err != nil {
        return nil, err
    }
    return &DB{db: db, encode: encode, decode: decode}, nil
}

//Close closes the database
func (db *DB) Close() error {
	return db.db.Close()
}

{{range $structName, $struct := .}}
{{if $struct.Exported}}
//Iter{{$structName}} iterates trough all {{$structName}} in db
type Iter{{$structName}} struct {
	db *DB
	it iterator.Iterator
}

//Next sets the iterator to the next {{$structName}}, or returns false
func (it *Iter{{$structName}}) Next() bool {
	return it.it.Next()
}

//ID returns {{$structName}} id
func (it *Iter{{$structName}}) ID() int {
	key := it.it.Key()
	index := bytes.LastIndexByte(key, '/')
	if index == -1 {
		return 0
	}
	objID, err := lexLoadInt(key[index+1:])
	if err != nil {
		return 0
	}
	return objID
}

//Value returns the {{$structName}} on witch is the iterator
func (it *Iter{{$structName}}) Value() (*{{$structName}}, error) {
	data := it.it.Value()
	var obj {{$structName}}
	err := it.db.decode(data, &obj)
	if err != nil {
		return nil, err
	}
	return &obj, nil
}

//IterIndex{{$structName}} iterates trough an index for {{$structName}} in db
type IterIndex{{$structName}} struct {
	Iter{{$structName}}
}

//Value returns the {{$structName}} on witch is the iterator
func (it *IterIndex{{$structName}}) Value() (*{{$structName}}, error) {
	key := it.it.Key()
	index := bytes.LastIndexByte(key, '/')
	if index == -1 {
		return nil, fmt.Errorf("Index for {{$structName}} has bad encoding")
	}
	id, err := lexLoadInt(key[index+1:])
	if err != nil {
		return nil, err
	}
	obj, err := it.db.Get{{$structName}}(id)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func prefix{{$structName}}(id int) []byte {
	buf := bytes.NewBuffer([]byte("{{$structName}}"))
	buf.WriteRune('/')
	buf.Write(lexDumpInt(id))
    return buf.Bytes()
}

//Get{{$structName}} returns the object with specified id or an error
func (db *DB) Get{{$structName}}(id int) (*{{$structName}}, error) {
	data, err := db.db.Get(prefix{{$structName}}(id), nil)
	if err != nil {
		return nil, err
	}
	var obj {{$structName}}
	err = db.decode(data, &obj)
	if err != nil {
		return nil, err
	}
	return &obj, nil
}

//Add{{$structName}} inserts new {{$structName}} to the db
func (db *DB) Add{{$structName}}(obj *{{$structName}}) (int, error) {
    data, err := db.encode(&obj)
    if err != nil {
        return 0, err
    }
    batch := new(leveldb.Batch)
    id := rand.Int()
    batch.Put(prefix{{$structName}}(id), data)
    err = db.add{{$structName}}Index([]byte("${{$structName}}"), batch, id, obj)
    if err != nil {
        return 0, err
    }
    err = db.db.Write(batch, nil)
    if err != nil {
        return 0, err
    }
    return id, nil
}

//Update{{$structName}} updates {{$structName}} with specified id
func (db *DB) Update{{$structName}}(id int, obj *{{$structName}}) error {
    key := prefix{{$structName}}(id)
    _, err := db.db.Get(key, nil)
    if err != nil {
        return err
    }
    data, err := db.encode(&obj)
    if err != nil {
        return err
    }
    batch := new(leveldb.Batch)
    batch.Put(key, data)
    err = db.remove{{$structName}}Index(batch, id)
    if err != nil {
        return err
    }
    err = db.add{{$structName}}Index([]byte("${{$structName}}"), batch, id, obj)
    if err != nil {
        return err
    }
    err = db.db.Write(batch, nil)
    if err != nil {
        return err
    }
    return nil
}

func (db *DB) remove{{$structName}}Index(batch *leveldb.Batch, id int) error {
    iter := db.db.NewIterator(util.BytesPrefix([]byte("${{$structName}}")), nil)
    for iter.Next() {
		key := iter.Key()
		index := bytes.LastIndexByte(key, '/')
		if index == -1 {
			return fmt.Errorf("Index for {{$structName}} has bad encoding")
		}
		objID, err := lexLoadInt(key[index+1:])
		if err != nil {
			return err
		}
        if err != nil {
            return err
        }
        if id == objID {
            batch.Delete(key)
        }
    }
    iter.Release()
    return iter.Error()
}

//Iter{{$structName}}All returns an iterator witch iterates trough all {{$structName}}s
func (db *DB) Iter{{$structName}}All() *Iter{{$structName}} {
	return &Iter{{$structName}}{
		it: db.db.NewIterator(util.BytesPrefix([]byte("{{$structName}}")), nil),
		db: db,
	}
}

{{range $indexName, $subType := (getIndex $structName)}}
//Iter{{$structName}}{{$indexName}}Eq iterates trough {{$structName}} {{$indexName}} index with equal values
func (db *DB) Iter{{$structName}}{{$indexName}}Eq(val {{$subType}}) *IterIndex{{$structName}} {
	valDump := lexDump{{title $subType}}(val)
	prefix := append([]byte("${{$structName}}/{{$indexName}}/"), valDump...)
	return &IterIndex{{$structName}}{
		Iter{{$structName}}{
			it: db.db.NewIterator(util.BytesPrefix(prefix), nil),
			db: db,
		},
	}
}
{{end}}
{{end}}

{{if hasIndex $structName}}
func (db *DB) add{{$structName}}Index(prefix []byte, batch *leveldb.Batch, id int, obj *{{$structName}}) (err error) {
	if obj == nil {
		return nil
	}
	var buf *bytes.Buffer
    {{range $attrName, $attr := $struct.Attrs}}
    {{if isStruct $attr.Type}}
		{{if hasIndex $attr.Type}}
		{{if (contains $attr.Type "[]")}}
			for _, attr := range obj.{{$attrName}} {
				err = db.add{{replace (replace $attr.Type "[]" "") "*" ""}}Index(prefix, batch, id, {{if not (contains $attr.Type "*")}}&{{end}}attr)
				if err != nil {
					return err
				}
			}
    	{{else}}
			err = db.add{{replace $attr.Type "*" ""}}Index(prefix, batch, id, obj.{{$attrName}})
			if err != nil {
				return err
			}
		{{end}}
		{{end}}
	{{else}}
	    {{if ne $attr.Index ""}}
			{{if (contains $attr.Type "[]")}}
				for _, attr := range obj.{{$attrName}} {
					buf = bytes.NewBuffer(prefix)
					buf.WriteRune('/')
					buf.WriteString("{{$attr.Index}}")
					buf.WriteRune('/')
					buf.Write(lexDump{{title (replace $attr.Type "[]" "")}}(attr))
					buf.WriteRune('/')
					buf.Write(lexDumpInt(id))
					batch.Put(buf.Bytes(), nil)
				}
			{{else}}
				buf = bytes.NewBuffer(prefix)
				buf.WriteRune('/')
				buf.WriteString("{{$attr.Index}}")
				buf.WriteRune('/')
				buf.Write(lexDump{{title $attr.Type}}(obj.{{$attrName}}))
				buf.WriteRune('/')
				buf.Write(lexDumpInt(id))
				batch.Put(buf.Bytes(), nil)
			{{end}}
		{{end}}
    {{end}}
    {{end}}
    return nil
}
{{end}}
{{end}}
`

//Parse parses the given file and returns the DBGenerator
func Parse(filename string) (DBGenerator, error) {
	gen := make(DBGenerator)
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("Error in parsing file %s: %s", filename, err)
	}
	ast.Inspect(f, func(node ast.Node) bool {
		typ, ok := node.(*ast.GenDecl)
		if !ok || typ.Tok != token.TYPE {
			return true
		}
		for _, spec := range typ.Specs {
			structSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			structType, ok := structSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			parseStructType(gen, structSpec, structType)
		}
		return true
	})
	return gen, nil
}

func parseStructType(gen DBGenerator, structSpec *ast.TypeSpec, structType *ast.StructType) {
	s := &Struct{
		Name:     structSpec.Name.Name,
		Exported: ast.IsExported(structSpec.Name.Name),
		Attrs:    make(map[string]*Attr),
	}
	gen[structSpec.Name.Name] = s
	for _, field := range structType.Fields.List {
		for _, name := range field.Names {
			attr := new(Attr)
			if field.Tag != nil && field.Tag.Kind == token.STRING {
				if field.Tag.Value[:8] == "`index:\"" {
					attr.Index = field.Tag.Value[8 : len(field.Tag.Value)-2]
				} else {
					fmt.Printf("WARNING: Field %s in struct %s has not valid tag (%s)", name.Name, structSpec.Name.Name, field.Tag.Value)
				}
			}
			switch typ := field.Type.(type) {
			case *ast.Ident:
				attr.Type = typ.Name
			case *ast.StarExpr:
				attr.Type = "*" + typ.X.(*ast.Ident).Name
			case *ast.ArrayType:
				switch expr := typ.Elt.(type) {
				case *ast.Ident:
					attr.Type = "[]" + expr.Name
				case *ast.StarExpr:
					attr.Type = "[]*" + expr.X.(*ast.Ident).Name
				default:
					panic("Not known type")
				}
			}
			s.Attrs[name.Name] = attr
		}
	}
}

//DBGenerator represents the structs in file
type DBGenerator map[string]*Struct

func (gen DBGenerator) String() string {
	var buf bytes.Buffer
	buf.WriteString("DBGenerator {\n")
	for structName, s := range gen {
		buf.WriteString(fmt.Sprintf("\t%s: {\n\t\tExported: %t\n", structName, s.Exported))
		for attrName, attr := range s.Attrs {
			buf.WriteString(fmt.Sprintf("\t\t%s %s", attrName, attr.Type))
			if attr.Index != "" {
				buf.WriteString(fmt.Sprintf(" index:%s", attr.Index))
			}
			buf.WriteRune('\n')
		}
		buf.WriteString("\t},\n")
	}
	buf.WriteString("}")
	return buf.String()
}

func (gen DBGenerator) hasIndex(structName string) bool {
	s, ok := gen[strings.Replace(strings.Replace(structName, "[]", "", -1), "*", "", -1)]
	if !ok {
		return false
	}
	for _, attr := range s.Attrs {
		if gen.isStruct(attr.Type) {
			return gen.hasIndex(attr.Type)
		}
		if attr.Index != "" {
			return true
		}
	}
	return false
}

func (gen DBGenerator) getIndex(structName string) map[string]string {
	s, ok := gen[strings.Replace(strings.Replace(structName, "[]", "", -1), "*", "", -1)]
	if !ok {
		return nil
	}
	ret := make(map[string]string)
	for _, attr := range s.Attrs {
		if gen.isStruct(attr.Type) {
			for k, v := range gen.getIndex(attr.Type) {
				ret[k] = v
			}
		} else if attr.Index != "" {
			ret[attr.Index] = attr.Type
		}
	}
	return ret
}

func (gen DBGenerator) isStruct(structName string) bool {
	_, ok := gen[strings.Replace(strings.Replace(structName, "[]", "", -1), "*", "", -1)]
	return ok
}

//Generate generates the db sourcecode
func (gen DBGenerator) Generate(w io.Writer) error {
	funcMap := template.FuncMap{
		"isStruct": gen.isStruct,
		"replace":  func(o, old, new string) string { return strings.Replace(o, old, new, -1) },
		"contains": strings.Contains,
		"hasIndex": gen.hasIndex,
		"getIndex": gen.getIndex,
		"title":    strings.Title,
	}
	buf := bytes.NewBuffer(nil)
	temp, err := template.New("").Funcs(funcMap).Parse(DBTEMPLATE)
	if err != nil {
		return err
	}
	err = temp.Execute(buf, gen)
	if err != nil {
		return fmt.Errorf("Template execution error: %v", err)
	}
	src, err := format.Source(buf.Bytes())
	if err != nil {
		return fmt.Errorf("format source error: $%v", err)
	}
	w.Write(src)
	return nil
}

//Struct TODO
type Struct struct {
	Name     string
	Exported bool
	Attrs    map[string]*Attr
}

//Attr is the attribute of a struct, its type and if the attribute will be indexed
type Attr struct {
	Type  string
	Index string
}
