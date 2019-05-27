package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/scanner"
	"text/template"

	"github.com/jadr2ddude/exp/conf"
)

// Type is a. . . type?
type Type interface {
	fmt.Stringer

	// yummy go syntax
	GoType() string
}

// PrimitiveType is a type which cannot be decomposed further.
type PrimitiveType string

func (pt PrimitiveType) String() string {
	return string(pt)
}

func (pt PrimitiveType) GoType() string {
	return pt.String()
}

// Primitive types
const (
	Uint8Type   PrimitiveType = "uint8"
	Uint16Type  PrimitiveType = "uint16"
	Uint32Type  PrimitiveType = "uint32"
	Uint64Type  PrimitiveType = "uint64"
	Int8Type    PrimitiveType = "int8"
	Int16Type   PrimitiveType = "int16"
	Int32Type   PrimitiveType = "int32"
	Int64Type   PrimitiveType = "int64"
	Float32Type PrimitiveType = "float32"
	Float64Type PrimitiveType = "float64"
	BoolType    PrimitiveType = "bool"
	ByteType    PrimitiveType = "byte"
	StringType  PrimitiveType = "string"
	StreamType  PrimitiveType = "stream"
)

// NamedType is a named type as the name implies.
type NamedType string

func (nt NamedType) String() string {
	return string(nt)
}

// GoType returns the Go representation of the type.
func (nt NamedType) GoType() string {
	return nt.String()
}

// ArrayType is a type containing multiple elements of the same underlying type.
type ArrayType struct {
	Elem Type
}

func (at ArrayType) String() string {
	return "[]" + at.Elem.String()
}

// Arg is an argument to an Op.
type Arg struct {
	// Name is the name of the argument.
	Name string

	// Type is the type of the argument.
	Type Type

	// Description is the human-readable description of the argument.
	// This is *NOT* optional.
	Description string
}

func (a *Arg) directive(dir string, pos scanner.Position, scan conf.Scanner) error {
	switch dir {
	case "name":
		if !scan.Next() {
			if err := scan.Err(); err != nil {
				return conf.WrapPos(err, pos)
			}
			return conf.WrapPos(errors.New("missing name argument"), pos)
		}
		name, err := conf.ScanString(scan)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		if a.Name != "" {
			return conf.WrapPos(errors.New("duplicate name directive"), pos)
		}
		a.Name = name
	case "type":
		if !scan.Next() {
			if err := scan.Err(); err != nil {
				return conf.WrapPos(err, pos)
			}
			return conf.WrapPos(errors.New("missing type argument"), pos)
		}
		var t Type
		switch scan.Tok() {
		case scanner.RawString:
			tstr := scan.Text()
			switch PrimitiveType(tstr) {
			case Uint8Type, Uint16Type, Uint32Type, Uint64Type:
				fallthrough
			case Int8Type, Int16Type, Int32Type, Int64Type:
				fallthrough
			case Float32Type, Float64Type:
				fallthrough
			case BoolType, ByteType, StringType:
				t = PrimitiveType(tstr)
			case StreamType:
				// TODO: streams
				return conf.WrapPos(errUnimplemented, scan.Pos())
			default:
				// TODO: named types
				return conf.WrapPos(errUnimplemented, scan.Pos())
			}
		default:
			return conf.Unexpected(scan)
		}
		if a.Type != nil {
			return conf.WrapPos(errors.New("duplicate type directive"), pos)
		}
		a.Type = t
	case "description", "desc":
		if !scan.Next() {
			if err := scan.Err(); err != nil {
				return conf.WrapPos(err, pos)
			}
			return conf.WrapPos(errors.New("missing description argument"), pos)
		}
		desc, err := conf.ScanString(scan)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		if a.Description == "" {
			a.Description = desc
		} else {
			a.Description += "\n" + desc
		}
	default:
		return conf.WrapPos(ErrInvalidDirective{dir}, pos)
	}

	// check for semicolon
	if scan.Next() {
		return conf.Unexpected(scan)
	} else if err := scan.Err(); err != nil {
		return conf.WrapPos(err, pos)
	}

	return nil
}

func (a *Arg) parse(scan conf.Scanner, pos scanner.Position) error {
	if !scan.Next() {
		if err := scan.Err(); err != nil {
			return conf.WrapPos(err, pos)
		}
		return conf.WrapPos(errors.New("missing operation definition"), pos)
	}
	switch scan.Tok() {
	case scanner.RawString, scanner.String:
		name, err := conf.ScanString(scan)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		a.Name = name
		if !scan.Next() {
			if err := scan.Err(); err != nil {
				return conf.WrapPos(err, pos)
			}
			return conf.WrapPos(errors.New("missing operation definition"), pos)
		}
		if scan.Tok() != '{' {
			return conf.Unexpected(scan)
		}
	case '{':
	default:
		return conf.Unexpected(scan)
	}
	bpos := scan.Pos()
	bscan := conf.ScanBracket(scan, '{', '}')
	for bscan.Next() {
		dir, err := conf.ScanString(bscan)
		if err != nil {
			return err
		}
		dir = strings.ToLower(dir)
		err = a.directive(dir, bscan.Pos(), conf.ScanSemicolon(bscan, openers, closers))
		if err != nil {
			return err
		}
	}
	if bscan.Err() != nil {
		return conf.WrapPos(bscan.Err(), bpos)
	}

	err := a.prep()
	if err != nil {
		return conf.WrapPos(err, pos)
	}

	return nil
}

func (a *Arg) prep() error {
	if a.Name == "" {
		return errors.New("argument missing name")
	}
	/*switch a.Type {
	case "":
		return fmt.Errorf("argument %q missing type", a.Name)
	case Uint8Type, Uint16Type, Uint32Type, Uint64Type,
		Int8Type, Int16Type, Int32Type, Int64Type,
		Float32Type, Float64Type,
		BoolType, ByteType, StringType, StreamType:
	default:
		return fmt.Errorf("argument %q has invalid type %q", a.Name, a.Type)
	}*/
	if a.Description == "" {
		return fmt.Errorf("argument %q missing description", a.Name)
	}
	return nil
}

// Error is a transferrable error.
type Error struct {
	// Name is the name of the argument.
	Name string

	// Fields is the type of the argument.
	Fields []Arg

	// Text is the human readable text with which the error is rendered.
	// Required.
	Text string

	// Description is the human-readable description of the argument.
	// This is *NOT* optional.
	Description string

	// Code is the corresponding HTTP status code.
	// Defaults to http.StatusInternalServerError.
	Code int
}

func (e *Error) directive(dir string, pos scanner.Position, scan conf.Scanner) error {
	switch dir {
	case "name":
		if !scan.Next() {
			if err := scan.Err(); err != nil {
				return conf.WrapPos(err, pos)
			}
			return conf.WrapPos(errors.New("missing name argument"), pos)
		}
		name, err := conf.ScanString(scan)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		if e.Name != "" {
			return conf.WrapPos(errors.New("duplicate name directive"), pos)
		}
		e.Name = name
	case "field":
		var a Arg
		err := a.parse(scan, pos)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		e.Fields = append(e.Fields, a)
	case "text":
		var txtdat string
		var set bool
		for scan.Next() {
			txt := scan.Text()
			switch scan.Tok() {
			case scanner.String:
				dtxt, err := conf.ScanString(scan)
				if err != nil {
					return conf.WrapPos(err, pos)
				}
				txt = dtxt
			}
			if !set {
				txtdat = txt
				set = true
			} else {
				txtdat += " " + txt
			}
		}
		if err := scan.Err(); err != nil {
			return conf.WrapPos(err, pos)
		}
		if !set {
			return conf.WrapPos(errors.New("missing text argument"), pos)
		}
		if e.Text != "" {
			return conf.WrapPos(errors.New("duplicate text directive"), pos)
		}
		e.Text = txtdat
		return nil
	case "description", "desc":
		if !scan.Next() {
			if err := scan.Err(); err != nil {
				return conf.WrapPos(err, pos)
			}
			return conf.WrapPos(errors.New("missing description argument"), pos)
		}
		desc, err := conf.ScanString(scan)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		if e.Description == "" {
			e.Description = desc
		} else {
			e.Description += "\n" + desc
		}
	case "code", "httpstatus":
		if !scan.Next() {
			if err := scan.Err(); err != nil {
				return conf.WrapPos(err, pos)
			}
			return conf.WrapPos(errors.New("missing code argument"), pos)
		}
		switch scan.Tok() {
		case scanner.Int:
			code, err := strconv.Atoi(scan.Text())
			if err != nil {
				return conf.WrapPos(err, scan.Pos())
			}
			if code < 100 || code >= 600 {
				return conf.WrapPos(fmt.Errorf("illegal http status code %d", code), scan.Pos())
			}
			e.Code = code
		case scanner.Float:
			return conf.WrapPos(errors.New("fractional http status codes are not a thing"), scan.Pos())
		default:
			return conf.Unexpected(scan)
		}
	default:
		return conf.WrapPos(ErrInvalidDirective{dir}, pos)
	}

	// check for semicolon
	if scan.Next() {
		return conf.Unexpected(scan)
	} else if err := scan.Err(); err != nil {
		return conf.WrapPos(err, pos)
	}

	return nil
}

func (e *Error) parse(scan conf.Scanner, pos scanner.Position) error {
	if !scan.Next() {
		if err := scan.Err(); err != nil {
			return conf.WrapPos(err, pos)
		}
		return conf.WrapPos(errors.New("missing error definition"), pos)
	}
	switch scan.Tok() {
	case scanner.RawString, scanner.String:
		name, err := conf.ScanString(scan)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		e.Name = name
		if !scan.Next() {
			if err := scan.Err(); err != nil {
				return conf.WrapPos(err, pos)
			}
			return conf.WrapPos(errors.New("missing error definition"), pos)
		}
		if scan.Tok() != '{' {
			return conf.Unexpected(scan)
		}
	case '{':
	default:
		return conf.Unexpected(scan)
	}
	bpos := scan.Pos()
	bscan := conf.ScanBracket(scan, '{', '}')
	for bscan.Next() {
		dir, err := conf.ScanString(bscan)
		if err != nil {
			return err
		}
		dir = strings.ToLower(dir)
		err = e.directive(dir, bscan.Pos(), conf.ScanSemicolon(bscan, openers, closers))
		if err != nil {
			return err
		}
	}
	if bscan.Err() != nil {
		return conf.WrapPos(bscan.Err(), bpos)
	}

	err := e.prep()
	if err != nil {
		return conf.WrapPos(err, pos)
	}

	return nil
}

func (e *Error) prep() error {
	if e.Name == "" {
		return errors.New("error misssing name")
	}
	if e.Fields == nil {
		e.Fields = []Arg{}
	}
	for i := range e.Fields {
		if err := e.Fields[i].prep(); err != nil {
			return err
		}
	}
	if e.Text == "" {
		return fmt.Errorf("error %q missing display text", e.Name)
	}
	if e.Description == "" {
		return fmt.Errorf("error %q missing description", e.Name)
	}
	if e.Code == 0 {
		e.Code = http.StatusInternalServerError
	}
	return nil
}

// Op is an HTTP handler RPC endpoint.
type Op struct {
	// Name is the name of the opetation.
	Name string

	// Description is the human-readable description of the operation.
	// This is *NOT* optional.
	Description string

	// Method is the HTTP request method.
	// Defaults to http.MethodHead if there are no inputs or outputs.
	// Otherwise defaults to http.MethodPost.
	Method string

	// ArgEncoding is an argument encoding system to use.
	// May be "query" or "json".
	// Defaults to "json" when the method is http.MethodPost.
	// Defaults to "query" when the method is http.MethodGet.
	ArgEncoding string

	// Path is the URL path of the endpoint.
	// Defaults to ".Name".
	Path string

	// Inputs is the set of inputs to the opetation.
	Inputs []Arg

	// Outputs is the set of outputs of the operation.
	Outputs []Arg

	// Errors is the set of possible errors which may occur during the operation.
	Errors []string
}

func (op *Op) directive(dir string, pos scanner.Position, scan conf.Scanner) error {
	switch dir {
	case "name":
		if !scan.Next() {
			if err := scan.Err(); err != nil {
				return conf.WrapPos(err, pos)
			}
			return conf.WrapPos(errors.New("missing name argument"), pos)
		}
		name, err := conf.ScanString(scan)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		if op.Name != "" {
			return conf.WrapPos(errors.New("duplicate name directive"), pos)
		}
		op.Name = name
	case "description", "desc":
		if !scan.Next() {
			if err := scan.Err(); err != nil {
				return conf.WrapPos(err, pos)
			}
			return conf.WrapPos(errors.New("missing description argument"), pos)
		}
		desc, err := conf.ScanString(scan)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		if op.Description == "" {
			op.Description = desc
		} else {
			op.Description += "\n" + desc
		}
	case "method":
		if !scan.Next() {
			if err := scan.Err(); err != nil {
				return conf.WrapPos(err, pos)
			}
			return conf.WrapPos(errors.New("missing method argument"), pos)
		}
		m, err := conf.ScanString(scan)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		m = strings.ToUpper(m)
		if op.Method != "" {
			return conf.WrapPos(errors.New("duplicate method directive"), pos)
		}
		op.Method = m
	case "argencoding", "encoding":
		if !scan.Next() {
			if err := scan.Err(); err != nil {
				return conf.WrapPos(err, pos)
			}
			return conf.WrapPos(errors.New("missing argument encoding argument"), pos)
		}
		enc, err := conf.ScanString(scan)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		switch enc {
		case "query", "json":
		default:
			return conf.WrapPos(fmt.Errorf("invalid argument encoding %q", enc), scan.Pos())
		}
		if op.ArgEncoding != "" {
			return conf.WrapPos(errors.New("duplicate encoding directive"), pos)
		}
		op.ArgEncoding = enc
	case "path":
		if !scan.Next() {
			if err := scan.Err(); err != nil {
				return conf.WrapPos(err, pos)
			}
			return conf.WrapPos(errors.New("missing path argument"), pos)
		}
		switch scan.Tok() {
		case scanner.String:

		case scanner.RawString:
			return conf.WrapPos(errors.New("unqouted paths are potentially dangerous; please quote the path"), scan.Pos())
		case '/':
			return conf.WrapPos(errors.New("unexpected token '/'; if this was supposed to be a path then please quote it"), scan.Pos())
		default:
			return conf.Unexpected(scan)
		}
		path, err := conf.ScanString(scan)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		u, err := url.Parse(path)
		if err != nil {
			return conf.WrapPos(err, scan.Pos())
		}
		switch {
		case u.Scheme != "":
			return conf.WrapPos(errors.New("path contains URL scheme; URL schemes not allowed"), scan.Pos())
		case u.Fragment != "":
			return conf.WrapPos(errors.New("path contains URL fragment; URL fragments not allowed"), scan.Pos())
		case u.Opaque != "":
			return conf.WrapPos(errors.New("path contains opaque URL data; URL opaque data not allowed"), scan.Pos())
		case u.User != nil:
			return conf.WrapPos(errors.New("path contains URL user info; URL user info not allowed"), scan.Pos())
		case u.Host != "":
			return conf.WrapPos(errors.New("path contains URL host; expected relative URL"), scan.Pos())
		case u.RawQuery != "":
			return conf.WrapPos(errors.New("path contains URL query; query not allowed"), scan.Pos())
		}
		if op.Path != "" {
			return errors.New("duplicate path directive")
		}
		op.Path = u.String()
	case "input", "in":
		var a Arg
		err := a.parse(scan, pos)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		op.Inputs = append(op.Inputs, a)
	case "output", "out":
		var a Arg
		err := a.parse(scan, pos)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		op.Outputs = append(op.Outputs, a)
	case "error", "err", "errors":
		var hasArg bool
		for scan.Next() {
			errname, err := conf.ScanString(scan)
			if err != nil {
				return err
			}

			for _, v := range op.Errors {
				if errname == v {
					return conf.WrapPos(fmt.Errorf("duplicate of error specification of %s", errname), scan.Pos())
				}
			}
			op.Errors = append(op.Errors, errname)
			hasArg = true
		}
		if err := scan.Err(); err != nil {
			return conf.WrapPos(err, pos)
		}
		if !hasArg {
			return conf.WrapPos(errors.New("missing error argument(s)"), pos)
		}
		return nil
	default:
		return conf.WrapPos(ErrInvalidDirective{dir}, pos)
	}

	// check for semicolon
	if scan.Next() {
		return conf.Unexpected(scan)
	} else if err := scan.Err(); err != nil {
		return conf.WrapPos(err, pos)
	}

	return nil
}

func (op *Op) parse(scan conf.Scanner, pos scanner.Position) error {
	if !scan.Next() {
		if err := scan.Err(); err != nil {
			return conf.WrapPos(err, pos)
		}
		return conf.WrapPos(errors.New("missing operation definition"), pos)
	}
	switch scan.Tok() {
	case scanner.RawString, scanner.String:
		name, err := conf.ScanString(scan)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		op.Name = name
		if !scan.Next() {
			if err := scan.Err(); err != nil {
				return conf.WrapPos(err, pos)
			}
			return conf.WrapPos(errors.New("missing operation definition"), pos)
		}
		if scan.Tok() != '{' {
			return conf.Unexpected(scan)
		}
	case '{':
	default:
		return conf.Unexpected(scan)
	}
	bpos := scan.Pos()
	bscan := conf.ScanBracket(scan, '{', '}')
	for bscan.Next() {
		dir, err := conf.ScanString(bscan)
		if err != nil {
			return err
		}
		dir = strings.ToLower(dir)
		err = op.directive(dir, bscan.Pos(), conf.ScanSemicolon(bscan, openers, closers))
		if err != nil {
			return err
		}
	}
	if bscan.Err() != nil {
		return conf.WrapPos(bscan.Err(), bpos)
	}

	err := op.prep()
	if err != nil {
		return conf.WrapPos(err, pos)
	}

	return nil
}

func (op *Op) prep() error {
	if op.Name == "" {
		return errors.New("op missing name")
	}
	if op.Description == "" {
		return fmt.Errorf("op %q missing description", op.Name)
	}
	if op.Method == "" {
		if len(op.Inputs) == 0 && len(op.Outputs) == 0 {
			op.Method = http.MethodHead
		} else {
			op.Method = http.MethodPost
		}
	}
	if op.ArgEncoding == "" {
		switch op.Method {
		case http.MethodPost:
			op.ArgEncoding = "json"
		case http.MethodGet:
			op.ArgEncoding = "query"
		}
	}
	if op.Path == "" {
		op.Path = op.Name
	}
	if op.Inputs == nil {
		op.Inputs = []Arg{}
	} else {
		for i := range op.Inputs {
			if err := op.Inputs[i].prep(); err != nil {
				return err
			}
		}
	}
	if op.Outputs == nil {
		op.Outputs = []Arg{}
	} else {
		for i := range op.Outputs {
			if err := op.Outputs[i].prep(); err != nil {
				return err
			}
		}
	}
	if op.Errors == nil {
		op.Errors = []string{}
	}
	return nil
}

// System is a specification of a system exposed over HTTP.
type System struct {
	// Name is the name of the system.
	Name string

	// GoPackage is the equivalent Go package name.
	GoPackage string

	// Description is the human-readable description of the operation.
	// This is *NOT* optional.
	Description string

	// Set of operations for the system.
	Operations []Op

	// Error type definitions.
	Errors []Error
}

func (s *System) directive(dir string, pos scanner.Position, scan conf.Scanner) error {
	switch dir {
	case "name":
		if !scan.Next() {
			if err := scan.Err(); err != nil {
				return conf.WrapPos(err, pos)
			}
			return conf.WrapPos(errors.New("missing name argument"), pos)
		}
		name, err := conf.ScanString(scan)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		if s.Name != "" {
			return conf.WrapPos(errors.New("duplicate name directive"), pos)
		}
		s.Name = name
	case "gopackage", "go":
		if !scan.Next() {
			if err := scan.Err(); err != nil {
				return conf.WrapPos(err, pos)
			}
			return conf.WrapPos(errors.New("missing GoPackage argument"), pos)
		}
		gopkgname, err := conf.ScanString(scan)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		if s.GoPackage != "" {
			return conf.WrapPos(errors.New("duplicate GoPackage directive"), pos)
		}
		s.GoPackage = gopkgname
	case "description", "desc":
		if !scan.Next() {
			if err := scan.Err(); err != nil {
				return conf.WrapPos(err, pos)
			}
			return conf.WrapPos(errors.New("missing description argument"), pos)
		}
		desc, err := conf.ScanString(scan)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		if s.Description == "" {
			s.Description = desc
		} else {
			s.Description += "\n" + desc
		}
	case "operation", "op":
		var op Op
		err := op.parse(scan, pos)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		s.Operations = append(s.Operations, op)
	case "error", "err":
		var e Error
		err := e.parse(scan, pos)
		if err != nil {
			return conf.WrapPos(err, pos)
		}
		s.Errors = append(s.Errors, e)
	default:
		return conf.WrapPos(ErrInvalidDirective{dir}, pos)
	}

	// check for semicolon
	if scan.Next() {
		return conf.Unexpected(scan)
	} else if err := scan.Err(); err != nil {
		return conf.WrapPos(err, pos)
	}

	return nil
}

func (s *System) parse(scan conf.Scanner) error {
	for scan.Next() {
		dir, err := conf.ScanString(scan)
		if err != nil {
			return err
		}
		dir = strings.ToLower(dir)
		err = s.directive(dir, scan.Pos(), conf.ScanSemicolon(scan, openers, closers))
		if err != nil {
			return err
		}
	}
	err := s.prep()
	if err != nil {
		return err
	}
	return nil
}

func (s *System) prep() error {
	if s.Name == "" {
		return errors.New("system is missing a name")
	}
	if s.GoPackage == "" {
		s.GoPackage = strings.ToLower(s.Name)
	}
	if s.Description == "" {
		return errors.New("system is missing a description")
	}
	if len(s.Operations) == 0 {
		return errors.New("system has no operations")
	}
	for i := range s.Operations {
		if err := s.Operations[i].prep(); err != nil {
			return err
		}
	}
	if s.Errors == nil {
		s.Errors = []Error{}
	} else {
		for i := range s.Errors {
			if err := s.Errors[i].prep(); err != nil {
				return err
			}
		}
	}
	return nil
}

var openers = []rune("({[")
var closers = []rune(")}]")

// ErrInvalidDirective is an error which occurs when an invalid directive is encountered.
type ErrInvalidDirective struct {
	Directive string
}

func (err ErrInvalidDirective) Error() string {
	return fmt.Sprintf("invalid directive %q", err.Directive)
}

var errUnimplemented = errors.New("not yet implemented")

func parseSystem(r io.Reader) (System, error) {
	gscan := &scanner.Scanner{
		Mode: scanner.ScanFloats |
			scanner.ScanStrings | scanner.ScanRawStrings |
			scanner.ScanComments | scanner.SkipComments,
	}
	if f, ok := r.(*os.File); ok {
		gscan.Position.Filename = f.Name()
	}
	scan := conf.Scan(gscan.Init(r))

	var sys System
	if err := sys.parse(scan); err != nil {
		return System{}, err
	}
	return sys, nil
}

var goHTTPStatTbl = map[int]string{
	http.StatusOK:                            "http.StatusOK",
	http.StatusGone:                          "http.StatusGone",
	http.StatusFound:                         "http.StatusFound",
	http.StatusTeapot:                        "http.StatusTeapot",
	http.StatusLocked:                        "http.StatusLocked",
	http.StatusIMUsed:                        "http.StatusIMUsed",
	http.StatusCreated:                       "http.StatusCreated",
	http.StatusSeeOther:                      "http.StatusSeeOther",
	http.StatusTooEarly:                      "http.StatusTooEarly",
	http.StatusConflict:                      "http.StatusConflict",
	http.StatusNotFound:                      "http.StatusNotFound",
	http.StatusContinue:                      "http.StatusContinue",
	http.StatusAccepted:                      "http.StatusAccepted",
	http.StatusUseProxy:                      "http.StatusUseProxy",
	http.StatusForbidden:                     "http.StatusForbidden",
	http.StatusNoContent:                     "http.StatusNoContent",
	http.StatusBadRequest:                    "http.StatusBadRequest",
	http.StatusBadGateway:                    "http.StatusBadGateway",
	http.StatusProcessing:                    "http.StatusProcessing",
	http.StatusMultiStatus:                   "http.StatusMultiStatus",
	http.StatusNotModified:                   "http.StatusNotModified",
	http.StatusNotExtended:                   "http.StatusNotExtended",
	http.StatusLoopDetected:                  "http.StatusLoopDetected",
	http.StatusResetContent:                  "http.StatusResetContent",
	http.StatusUnauthorized:                  "http.StatusUnauthorized",
	http.StatusNotAcceptable:                 "http.StatusNotAcceptable",
	http.StatusPartialContent:                "http.StatusPartialContent",
	http.StatusGatewayTimeout:                "http.StatusGatewayTimeout",
	http.StatusLengthRequired:                "http.StatusLengthRequired",
	http.StatusNotImplemented:                "http.StatusNotImplemented",
	http.StatusRequestTimeout:                "http.StatusRequestTimeout",
	http.StatusAlreadyReported:               "http.StatusAlreadyReported",
	http.StatusUpgradeRequired:               "http.StatusUpgradeRequired",
	http.StatusPaymentRequired:               "http.StatusPaymentRequired",
	http.StatusMultipleChoices:               "http.StatusMultipleChoices",
	http.StatusTooManyRequests:               "http.StatusTooManyRequests",
	http.StatusFailedDependency:              "http.StatusFailedDependency",
	http.StatusMethodNotAllowed:              "http.StatusMethodNotAllowed",
	http.StatusMovedPermanently:              "http.StatusMovedPermanently",
	http.StatusProxyAuthRequired:             "http.StatusProxyAuthRequired",
	http.StatusRequestURITooLong:             "http.StatusRequestURITooLong",
	http.StatusPermanentRedirect:             "http.StatusPermanentRedirect",
	http.StatusExpectationFailed:             "http.StatusExpectationFailed",
	http.StatusTemporaryRedirect:             "http.StatusTemporaryRedirect",
	http.StatusMisdirectedRequest:            "http.StatusMisdirectedRequest",
	http.StatusPreconditionFailed:            "http.StatusPreconditionFailed",
	http.StatusServiceUnavailable:            "http.StatusServiceUnavailable",
	http.StatusSwitchingProtocols:            "http.StatusSwitchingProtocols",
	http.StatusUnprocessableEntity:           "http.StatusUnprocessableEntity",
	http.StatusInternalServerError:           "http.StatusInternalServerError",
	http.StatusInsufficientStorage:           "http.StatusInsufficientStorage",
	http.StatusNonAuthoritativeInfo:          "http.StatusNonAuthoritativeInfo",
	http.StatusUnsupportedMediaType:          "http.StatusUnsupportedMediaType",
	http.StatusPreconditionRequired:          "http.StatusPreconditionRequired",
	http.StatusRequestEntityTooLarge:         "http.StatusRequestEntityTooLarge",
	http.StatusVariantAlsoNegotiates:         "http.StatusVariantAlsoNegotiates",
	http.StatusHTTPVersionNotSupported:       "http.StatusHTTPVersionNotSupported",
	http.StatusUnavailableForLegalReasons:    "http.StatusUnavailableForLegalReasons",
	http.StatusRequestHeaderFieldsTooLarge:   "http.StatusRequestHeaderFieldsTooLarge",
	http.StatusRequestedRangeNotSatisfiable:  "http.StatusRequestedRangeNotSatisfiable",
	http.StatusNetworkAuthenticationRequired: "http.StatusNetworkAuthenticationRequired",
}

func main() {
	var spec string
	var tmplpath string
	var out string
	flag.StringVar(&spec, "spec", "", "path to spec to use")
	flag.StringVar(&tmplpath, "tmpl", "", "path to template to use")
	flag.StringVar(&out, "o", "", "path to output file")
	flag.Parse()

	sf, err := os.Open(spec)
	if err != nil {
		panic(err)
	}
	defer sf.Close()

	sys, err := parseSystem(sf)
	if err != nil {
		panic(err)
	}
	tmpl := template.New("")
	tmpl, err = tmpl.Funcs(template.FuncMap{
		"lines":    func(str string) []string { return strings.Split(str, "\n") },
		"httpcode": http.StatusText,
		"gohttpmethod": func(str string) string {
			switch str {
			case "":
				panic(errors.New("empty method"))
			case http.MethodGet:
				return "http.MethodGet"
			case http.MethodPost:
				return "http.MethodPost"
			case http.MethodHead:
				return "http.MethodHead"
			default:
				return fmt.Sprintf("%q", str)
			}
		},
		"gohttpstatus": func(code int) string {
			str, ok := goHTTPStatTbl[code]
			if !ok {
				return strconv.Itoa(code)
			}
			return str
		},
		"gozero": func(t Type) string {
			switch t {
			case Uint8Type, Uint16Type, Uint32Type, Uint64Type,
				Int8Type, Int16Type, Int32Type, Int64Type, ByteType:
				return "0"
			case Float32Type, Float64Type:
				return "0.0"
			case BoolType:
				return "false"
			case StringType:
				return `""`
			default:
				panic(errors.New("unsupported type"))
			}
		},
	}).ParseFiles(tmplpath)
	if err != nil {
		panic(err)
	}

	of, err := os.Create(out)
	if err != nil {
		panic(err)
	}
	defer of.Close()

	cmd := exec.Command("gofmt", "/dev/stdin")
	cmd.Stderr = os.Stderr
	cmd.Stdout = of
	fmw, err := cmd.StdinPipe()
	if err != nil {
		panic(err)
	}
	defer fmw.Close()
	err = cmd.Start()
	if err != nil {
		panic(err)
	}
	defer cmd.Wait()
	defer fmw.Close()

	err = tmpl.ExecuteTemplate(fmw, filepath.Base(tmplpath), sys)
	if err != nil {
		panic(err)
	}
	err = fmw.Close()
	if err != nil {
		panic(err)
	}
	err = cmd.Wait()
	if err != nil {
		panic(err)
	}
	err = of.Close()
	if err != nil {
		panic(err)
	}
}
