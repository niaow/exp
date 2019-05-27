package conf

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"text/scanner"
)

// Scanner is an interface for parsing things.
type Scanner interface {
	// Next attempts to advance to the next token, and returns whether it is available.
	// Any errors encountered will be stored and may be retrieved with a call to Err.
	Next() bool

	// Tok returns the token character corresponding to the token.
	// See scanner.Next for more info.
	Tok() rune

	// Text returns any text associated with the token.
	Text() string

	// Pos returns the position of the current token or error.
	Pos() scanner.Position

	// Err returns the current error, if present.
	// If no error has occured, then it will return false.
	Err() error
}

// PosErr is an error annotated with a position.
type PosErr struct {
	// Pos is the position at which the error was encountered.
	Pos scanner.Position

	// Err is the error encountered.
	Err error
}

// ErrUnexpectedToken is error which occurs when an unexpected token is encountered.
type ErrUnexpectedToken struct {
	Tok  rune
	Text string
}

func (err ErrUnexpectedToken) Error() string {
	switch err.Tok {
	case scanner.Int:
		return fmt.Sprintf("unexpected integer %s", err.Text)
	case scanner.Float:
		return fmt.Sprintf("unexpected number %s", err.Text)
	case scanner.Char:
		return fmt.Sprintf("unexpected character %s", err.Text)
	case scanner.String:
		return fmt.Sprintf("unexpected string %s", err.Text)
	case scanner.RawString:
		return fmt.Sprintf("unexpected token %q", err.Text)
	default:
		return fmt.Sprintf("unexpected token %q", err.Tok)
	}
}

// WrapPos wraps an error at a given position.
func WrapPos(err error, pos scanner.Position) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(PosErr); ok {
		return err
	}
	return PosErr{
		Pos: pos,
		Err: err,
	}
}

// Unexpected returns a ErrUnexpectedToken wrapped with an error position for the current token in the Scanner.
func Unexpected(s Scanner) error {
	return WrapPos(ErrUnexpectedToken{
		Tok:  s.Tok(),
		Text: s.Text(),
	}, s.Pos())
}

func (err PosErr) Error() string {
	return fmt.Sprintf("%s (%s)", err.Err.Error(), err.Pos.String())
}

type rawScanner struct {
	s   *scanner.Scanner
	tok rune
	err error
}

func (rs *rawScanner) scanConf() *rawScanner {
	rs.s.Error = func(s *scanner.Scanner, msg string) {
		rs.err = WrapPos(errors.New(msg), s.Pos())
	}
	return rs
}

func (rs *rawScanner) Next() bool {
	if rs.err != nil {
		return false
	}
	rs.tok = rs.s.Scan()
	if rs.err != nil {
		return false
	}
	if rs.tok == scanner.EOF {
		rs.err = io.EOF
		return false
	}
	return true
}

func (rs *rawScanner) Tok() rune {
	return rs.tok
}

func (rs *rawScanner) Text() string {
	return rs.s.TokenText()
}

func (rs *rawScanner) Pos() scanner.Position {
	return rs.s.Pos()
}

func (rs *rawScanner) Err() error {
	return rs.Err()
}

// Scan wraps a scanner.Scanner into a Scanner.
func Scan(s *scanner.Scanner) Scanner {
	return (&rawScanner{s: s}).scanConf()
}

type bracketScanner struct {
	Scanner
	level      uint
	err        error
	otok, ctok rune
}

func (bs *bracketScanner) Next() bool {
	if bs.level == 0 {
		return false
	}
	if !bs.Scanner.Next() {
		if bs.Scanner.Err() == nil {
			bs.err = WrapPos(io.ErrUnexpectedEOF, bs.Pos())
		}
		return false
	}
	switch bs.Tok() {
	case bs.otok:
		bs.level++
	case bs.ctok:
		bs.level--
	}
	if bs.level == 0 {
		return false
	}
	return true
}

func (bs *bracketScanner) Err() error {
	if bs.err != nil {
		return bs.err
	}
	return bs.Scanner.Err()
}

// ScanBracket returns a Scanner that reads tokens between two brackets.
// Must be called while parent scanner is on the open bracket.
func ScanBracket(parent Scanner, open rune, close rune) Scanner {
	return &bracketScanner{
		Scanner: parent,
		level:   1,
		otok:    open,
		ctok:    close,
	}
}

type semicolonScanner struct {
	Scanner
	level   int
	err     error
	openers map[rune]struct{}
	closers map[rune]struct{}
}

func (ss *semicolonScanner) Next() bool {
	if !ss.Scanner.Next() {
		if ss.Scanner.Err() == nil {
			ss.err = WrapPos(io.ErrUnexpectedEOF, ss.Pos())
		}
		return false
	}
	tok := ss.Scanner.Tok()
	if tok == ';' && ss.level == 0 {
		return false
	}
	if _, ok := ss.openers[tok]; ok {
		ss.level++
	}
	if _, ok := ss.closers[tok]; ok {
		ss.level--
	}
	return false
}

func (ss *semicolonScanner) Err() error {
	if ss.err != nil {
		return ss.err
	}
	return ss.Scanner.Err()
}

func mapRunes(runes []rune) map[rune]struct{} {
	if len(runes) == 0 {
		return map[rune]struct{}{}
	}

	m := map[rune]struct{}{}
	for _, r := range runes {
		m[r] = struct{}{}
	}
	return m
}

// ScanSemicolon returns a Scanner that scans until a semicolon.
// It can handle subcontexts, delimited by the given openers and closers.
func ScanSemicolon(parent Scanner, openers []rune, closers []rune) Scanner {
	return &semicolonScanner{
		Scanner: parent,
		openers: mapRunes(openers),
		closers: mapRunes(closers),
	}
}

// ScanString reads a string-ish token and returns the fully parsed string.
// If the token is a raw string, it will return the raw string.
// If the token is a quoted string, it will be unquoted.
// If the token is not string-ish, it will return an error.
func ScanString(scan Scanner) (string, error) {
	switch scan.Tok() {
	case scanner.RawString:
		return scan.Text(), nil
	case scanner.String:
		str, err := strconv.Unquote(scan.Text())
		if err != nil {
			return "", WrapPos(err, scan.Pos())
		}
		return str, nil
	default:
		return "", Unexpected(scan)
	}
}
